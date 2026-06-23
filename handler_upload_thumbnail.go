package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	userID, err := uuid.Parse(r.Header.Get("X-Tubely-UserID"))

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to retrieve X-Tubely-UserID header", err)
	}

	video, err := cfg.db.GetVideo(videoID)

	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to retrieve video", err)
		return
	}

	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Not your video", fmt.Errorf("Video %s does not belong to this user", videoID.String()))
		return
	}

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	maxSize := 10 << 20
	if r.ContentLength > int64(maxSize) {
		respondWithError(w, http.StatusBadRequest, "Invalid upload size (>10MB)", fmt.Errorf("Invalid upload size, max size is %d, found %d", maxSize, r.ContentLength))
		return
	}

	err = r.ParseMultipartForm(int64(maxSize))

	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse data", err)
		return
	}

	fileHeaders, present := r.MultipartForm.File["thumbnail"]

	if !present {
		respondWithError(w, http.StatusBadRequest, "No thumbnail field submitted", fmt.Errorf("No thumbnail file provided"))
		return
	}

	if len(fileHeaders) > 1 {
		respondWithError(w, http.StatusBadRequest, "Too many thumbnail files submitted", fmt.Errorf("To many thumbnail files submitted: expected 1, found %d", len(fileHeaders)))
		return
	}

	fileHeader := fileHeaders[0]

	if fileHeader.Size > r.ContentLength {
		respondWithError(w, http.StatusBadRequest, "Invalid thumbnail size", fmt.Errorf("Expected thumbnail size to be at most %d, found %d", r.ContentLength, fileHeader.Size))
		return
	}

	mediaTypeRaw := fileHeader.Header.Get("Content-Type")

	if mediaTypeRaw == "" {
		respondWithError(w, http.StatusBadRequest, "No thumbnail MIME type specified", fmt.Errorf("No thumbnail MIME type specified"))
		return
	}

	mediaType, _, err := mime.ParseMediaType(mediaTypeRaw)

	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Malformed thumbnail MIME type", err)
		return
	}

	switch mediaType {
	case "image/png":
	case "image/jpeg":
		break
	default:
		respondWithError(w, http.StatusBadRequest, "Invalid thumbnail type", fmt.Errorf("Expected image/png or image/jpeg, found %s", mediaType))
		return
	}

	file, err := fileHeader.Open()

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to open thumbnail file", err)
		return
	}

	defer file.Close()

	data := make([]byte, fileHeader.Size)
	n, err := file.Read(data)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error while reading thumbnail data", err)
		return
	}

	if n != int(fileHeader.Size) {
		respondWithError(w, http.StatusBadRequest, "Invalid thumbnail size", fmt.Errorf("Expected thumbnail size to be %d, read %d", fileHeader.Size, n))
		return
	}

	typeParts := strings.Split(mediaType, "/")

	fileEnding := typeParts[1]

	maskBytes := make([]byte, 32)
	_, err = rand.Read(maskBytes)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to generate file name", err)
		return
	}

	fileName := fmt.Sprintf("%s.%s", base64.RawURLEncoding.EncodeToString(maskBytes), fileEnding)

	file_path := path.Join(cfg.assetsRoot, fileName)

	err = os.WriteFile(file_path, data, fs.ModePerm)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to save the thumbnail", err)
		return
	}

	newUrl := fmt.Sprintf("%s://%s:%s/assets/%s", cfg.protocol, cfg.hostname, cfg.port, fileName)
	video.ThumbnailURL = &newUrl
	cfg.db.UpdateVideo(video)

	respondWithJSON(w, http.StatusOK, video)
}
