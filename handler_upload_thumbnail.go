package main

import (
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/Adicitus/bootdotdev.tubely/internal/auth"
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

	type_parts := strings.Split(mediaType, "/")

	file_ending := type_parts[1]
	file_path := path.Join(cfg.assetsRoot, fmt.Sprintf("%s.%s", videoIDString, file_ending))

	err = os.WriteFile(file_path, data, fs.ModePerm)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to save the thumbnail", err)
		return
	}

	newUrl := fmt.Sprintf("%s://%s:%s/assets/%s.%s", cfg.protocol, cfg.hostname, cfg.port, videoIDString, file_ending)
	video.ThumbnailURL = &newUrl
	cfg.db.UpdateVideo(video)

	respondWithJSON(w, http.StatusOK, video)
}
