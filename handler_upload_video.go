package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	const maxMemory = 1 << 30
	r.Body = http.MaxBytesReader(w, r.Body, maxMemory)
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}
	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}
	metadata, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error getting video metadata", err)
		return
	}
	if metadata.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "User is not the video owner", err)
		return
	}
	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error extracting video", err)
		return
	}
	defer file.Close()
	contentType := header.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Improper video type", err)
		return
	}
	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error creating temporary video file", err)
		return
	}
	defer os.Remove(tempFile.Name())
	_, err = io.Copy(tempFile, file)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error saving video", err)
		return
	}
	_, file_extension, found := strings.Cut(contentType, "/")
	if !found {
		respondWithError(w, http.StatusBadRequest, "Unable to save file", err)
		return
	}
	randBytes := make([]byte, 32)
	_, err = rand.Read(randBytes)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error creating bytes", err)
		return
	}
	fileKey := base64.RawURLEncoding.EncodeToString(randBytes)
	var prefix string
	aspectRatio, err := getVideoAspectRatio(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error getting aspect ratio", err)
		return
	}
	switch aspectRatio {
	case "16:9":
		prefix = "landscape"
	case "9:16":
		prefix = "portrait"
	default:
		prefix = "other"
	}
	file_path := fmt.Sprintf("%v/%v.%v", prefix, fileKey, file_extension)
	outputFilePath, err := processVideoForFastStart(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error getting output file path", err)
		fmt.Printf("This is the output file path: %v", outputFilePath)
		return
	}
	fileBody, err := os.Open(outputFilePath)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error opening temp file", err)
		return
	}
	defer fileBody.Close()
	defer os.Remove(outputFilePath)
	input := &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &file_path,
		Body:        fileBody,
		ContentType: &contentType,
	}
	ctx := context.Background()
	_, err = cfg.s3Client.PutObject(ctx, input)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error saving video", err)
		return
	}
	bucket := cfg.s3Bucket
	videoUrl := fmt.Sprintf("%v,%v", bucket, file_path)
	metadata.VideoURL = &videoUrl
	video, err := cfg.dbVideoToSignedVideo(metadata)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error saving video", err)
		return
	}
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error updating video", err)
		return
	}
	respondWithJSON(w, http.StatusOK, video)
}

func processVideoForFastStart(filePath string) (string, error) {
	outputFilePath := filePath + ".processing"
	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputFilePath)
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return outputFilePath, nil
}

func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	presignedClient := s3.NewPresignClient(s3Client)
	ctx := context.Background()
	params := &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	}
	request, err := presignedClient.PresignGetObject(ctx, params, s3.WithPresignExpires(expireTime))
	if err != nil {
		return "", err
	}
	return request.URL, nil
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	if video.VideoURL == nil {
		return video, nil
	}
	videoURL := *video.VideoURL
	splitStrings := strings.Split(videoURL, ",")
	if len(splitStrings) < 2 {
		return video, nil
	}
	presignedURL, err := generatePresignedURL(cfg.s3Client, splitStrings[0], splitStrings[1], time.Hour)
	if err != nil {
		return video, err
	}
	video.VideoURL = &presignedURL
	return video, nil
}
