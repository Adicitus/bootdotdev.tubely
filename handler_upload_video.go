package main

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
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

	fmt.Println("uploading video file for video", videoID, "by user", userID)

	maxSize := 1 << 30
	if r.ContentLength > int64(maxSize) {
		respondWithError(w, http.StatusBadRequest, "Invalid upload size (>10MB)", fmt.Errorf("Invalid upload size, max size is %d, found %d", maxSize, r.ContentLength))
		return
	}

	err = r.ParseMultipartForm(int64(maxSize))

	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse data", err)
		return
	}

	fileHeaders, present := r.MultipartForm.File["video"]

	if !present {
		respondWithError(w, http.StatusBadRequest, "No video-file field submitted", fmt.Errorf("No thumbnail file provided"))
		return
	}

	if len(fileHeaders) > 1 {
		respondWithError(w, http.StatusBadRequest, "Too many video files submitted", fmt.Errorf("To many thumbnail files submitted: expected 1, found %d", len(fileHeaders)))
		return
	}

	fileHeader := fileHeaders[0]

	if fileHeader.Size > r.ContentLength {
		respondWithError(w, http.StatusBadRequest, "Invalid video size", fmt.Errorf("Expected thumbnail size to be at most %d, found %d", r.ContentLength, fileHeader.Size))
		return
	}

	mediaTypeRaw := fileHeader.Header.Get("Content-Type")

	if mediaTypeRaw == "" {
		respondWithError(w, http.StatusBadRequest, "No video MIME type specified", fmt.Errorf("No thumbnail MIME type specified"))
		return
	}

	mediaType, _, err := mime.ParseMediaType(mediaTypeRaw)

	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Malformed video MIME type", err)
		return
	}

	switch mediaType {
	case "video/mp4":
		break
	default:
		respondWithError(w, http.StatusBadRequest, "Invalid video type", fmt.Errorf("Expected image/png or image/jpeg, found %s", mediaType))
		return
	}

	extension := strings.Split(mediaType, "/")[1]

	filename := fmt.Sprintf("%s.%s", videoID.String(), extension)
	tempFile, err := os.CreateTemp("", filename)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to allocate tempoary storage", err)
		return
	}

	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	inFile, err := fileHeader.Open()

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to access uploaded file", err)
	}

	n, err := io.Copy(tempFile, inFile)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to load uploaded file", err)
		return
	}

	if n != fileHeader.Size {
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Read an unexpected number of bytes: read %d, expected %d", n, fileHeader.Size), nil)
		return
	}

	_, err = tempFile.Seek(0, io.SeekStart)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed IO operation on temporary storage", err)
		return
	}

	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &filename,
		Body:        tempFile,
		ContentType: &mediaType,
	})

	if err != nil {
		respondWithError(w, http.StatusBadGateway, "Failed to commit video to long-term storage", err)
		return
	}

	s3Url := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, filename)
	video.VideoURL = &s3Url

	err = cfg.db.UpdateVideo(video)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to update video metadata", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}
