package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
	"fmt"
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
	tempFile.Seek(0, io.SeekStart)
	randBytes := make([]byte, 32)
	rand.Read(randBytes)
	fileKey := base64.RawURLEncoding.EncodeToString(randBytes)
	file_path := fmt.Sprintf("%v.%v", fileKey, file_extension)
	input := &s3.PutObjectInput{
		Bucket: &cfg.s3Bucket,
		Key: &file_path,
		Body: tempFile,
		ContentType: &contentType,
	}
	ctx := context.Background()
	_, err = cfg.s3Client.PutObject(ctx, input)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error saving video", err)
		return
	}
	bucket := cfg.s3Bucket
	region := cfg.s3Region
	videoUrl := fmt.Sprintf("https://%v.s3.%v.amazonaws.com/%v.%v", bucket, region, fileKey, file_extension)
	metadata.VideoURL = &videoUrl
	err = cfg.db.UpdateVideo(metadata)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error updating video", err)
		return
	}
	respondWithJSON(w, http.StatusOK, metadata)
}
