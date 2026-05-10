package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
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

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	const maxMemory = 10 << 20
	r.ParseMultipartForm(maxMemory)

	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	mediatype, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error parsing media type", err)
		return
	}
	if mediatype != "image/jpeg" && mediatype != "image/png" {
		respondWithError(w, http.StatusBadRequest, "Improper media type media type", err)
		return
	}
	
	metadata, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized user", err)
		return
	}
	if metadata.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized user", err)
		return
	}
	_, file_extension, found := strings.Cut(contentType, "/")
	if !found {
		respondWithError(w, http.StatusBadRequest, "Unable to save file", err)
		return
	}
	randomBytes := make([]byte, 32)
	rand.Read(randomBytes)
	video_path := base64.RawURLEncoding.EncodeToString(randomBytes)
	file_path := fmt.Sprintf("/%v.%v", video_path, file_extension)
	file_path = filepath.Join(cfg.assetsRoot, file_path)
	dest, err := os.Create(file_path)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error saving thumbnail", err)
		return
	}
	_, err = io.Copy(dest, file)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error saving thumbnail", err)
		return
	}
	thumbnail_url := fmt.Sprintf("http://localhost:%v/assets/%v.%v", cfg.port, video_path, file_extension)
	metadata.ThumbnailURL = &thumbnail_url
	err = cfg.db.UpdateVideo(metadata)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error updating video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, metadata)
}
