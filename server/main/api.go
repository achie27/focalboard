package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
)

// ----------------------------------------------------------------------------------------------------
// REST APIs

type API struct {
	appBuilder func() *App
}

func NewAPI(appBuilder func() *App) *API {
	return &API{appBuilder: appBuilder}
}

func (a *API) app() *App {
	return a.appBuilder()
}

func (a *API) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/api/v1/blocks", a.handleGetBlocks).Methods("GET")
	r.HandleFunc("/api/v1/blocks", a.handlePostBlocks).Methods("POST")
	r.HandleFunc("/api/v1/blocks/{blockID}", a.handleDeleteBlock).Methods("DELETE")
	r.HandleFunc("/api/v1/blocks/{blockID}/subtree", a.handleGetSubTree).Methods("GET")

	r.HandleFunc("/api/v1/files", a.handleUploadFile).Methods("POST")
	r.HandleFunc("/files/{filename}", a.handleServeFile).Methods("GET")

	r.HandleFunc("/api/v1/blocks/export", a.handleExport).Methods("GET")
	r.HandleFunc("/api/v1/blocks/import", a.handleImport).Methods("POST")
}

func (a *API) handleGetBlocks(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	parentID := query.Get("parent_id")
	blockType := query.Get("type")

	blocks, err := a.app().GetBlocks(parentID, blockType)
	if err != nil {
		log.Printf(`ERROR GetBlocks: %v`, r)
		errorResponse(w, http.StatusInternalServerError, `{}`)
		return
	}

	log.Printf("GetBlocks parentID: %s, type: %s, %d result(s)", parentID, blockType, len(blocks))
	json, err := json.Marshal(blocks)
	if err != nil {
		log.Printf(`ERROR json.Marshal: %v`, r)
		errorResponse(w, http.StatusInternalServerError, `{}`)
		return
	}

	jsonBytesResponse(w, http.StatusOK, json)
}

func (a *API) handlePostBlocks(w http.ResponseWriter, r *http.Request) {
	requestBody, err := ioutil.ReadAll(r.Body)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, `{}`)
		return
	}

	// Catch panics from parse errors, etc.
	defer func() {
		if r := recover(); r != nil {
			log.Printf(`ERROR: %v`, r)
			errorResponse(w, http.StatusInternalServerError, `{}`)
			return
		}
	}()

	var blocks []Block
	err = json.Unmarshal([]byte(requestBody), &blocks)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, ``)
		return
	}

	for _, block := range blocks {
		// Error checking
		if len(block.Type) < 1 {
			errorResponse(w, http.StatusInternalServerError, fmt.Sprintf(`{"description": "missing type", "id": "%s"}`, block.ID))
			return
		}
		if block.CreateAt < 1 {
			errorResponse(w, http.StatusInternalServerError, fmt.Sprintf(`{"description": "invalid createAt", "id": "%s"}`, block.ID))
			return
		}
		if block.UpdateAt < 1 {
			errorResponse(w, http.StatusInternalServerError, fmt.Sprintf(`{"description": "invalid updateAt", "id": "%s"}`, block.ID))
			return
		}

	}

	err = a.app().InsertBlocks(blocks)
	if err != nil {
		log.Printf(`ERROR: %v`, r)
		errorResponse(w, http.StatusInternalServerError, `{}`)
		return
	}

	log.Printf("POST Blocks %d block(s)", len(blocks))
	jsonStringResponse(w, http.StatusOK, "{}")
}

func (a *API) handleDeleteBlock(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	blockID := vars["blockID"]

	err := a.app().DeleteBlock(blockID)
	if err != nil {
		log.Printf(`ERROR: %v`, r)
		errorResponse(w, http.StatusInternalServerError, `{}`)
		return
	}

	log.Printf("DELETE Block %s", blockID)
	jsonStringResponse(w, http.StatusOK, "{}")
}

func (a *API) handleGetSubTree(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	blockID := vars["blockID"]

	blocks, err := a.app().GetSubTree(blockID)
	if err != nil {
		log.Printf(`ERROR: %v`, r)
		errorResponse(w, http.StatusInternalServerError, `{}`)
		return
	}

	log.Printf("GetSubTree blockID: %s, %d result(s)", blockID, len(blocks))
	json, err := json.Marshal(blocks)
	if err != nil {
		log.Printf(`ERROR json.Marshal: %v`, r)
		errorResponse(w, http.StatusInternalServerError, `{}`)
		return
	}

	jsonBytesResponse(w, http.StatusOK, json)
}

func (a *API) handleExport(w http.ResponseWriter, r *http.Request) {
	blocks, err := a.app().GetAllBlocks()
	if err != nil {
		log.Printf(`ERROR: %v`, r)
		errorResponse(w, http.StatusInternalServerError, `{}`)
		return
	}

	log.Printf("EXPORT Blocks, %d result(s)", len(blocks))
	json, err := json.Marshal(blocks)
	if err != nil {
		log.Printf(`ERROR json.Marshal: %v`, r)
		errorResponse(w, http.StatusInternalServerError, `{}`)
		return
	}

	jsonBytesResponse(w, http.StatusOK, json)
}

func (a *API) handleImport(w http.ResponseWriter, r *http.Request) {
	requestBody, err := ioutil.ReadAll(r.Body)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, `{}`)
		return
	}

	// Catch panics from parse errors, etc.
	defer func() {
		if r := recover(); r != nil {
			log.Printf(`ERROR: %v`, r)
			errorResponse(w, http.StatusInternalServerError, `{}`)
			return
		}
	}()

	var blocks []Block
	err = json.Unmarshal([]byte(requestBody), &blocks)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, ``)
		return
	}

	for _, block := range blocks {
		err := a.app().InsertBlock(block)
		if err != nil {
			log.Printf(`ERROR: %v`, r)
			errorResponse(w, http.StatusInternalServerError, `{}`)
			return
		}
	}

	log.Printf("IMPORT Blocks %d block(s)", len(blocks))
	jsonStringResponse(w, http.StatusOK, "{}")
}

// File upload

func (a *API) handleServeFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	filename := vars["filename"]

	contentType := "image/jpg"
	fileExtension := strings.ToLower(filepath.Ext(filename))
	if fileExtension == "png" {
		contentType = "image/png"
	}

	w.Header().Set("Content-Type", contentType)

	folderPath := config.FilesPath
	filePath := filepath.Join(folderPath, filename)
	http.ServeFile(w, r, filePath)
}

func (a *API) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	fmt.Println(`handleUploadFile`)
	file, handle, err := r.FormFile("file")
	if err != nil {
		fmt.Fprintf(w, "%v", err)
		return
	}
	defer file.Close()

	log.Printf(`handleUploadFile, filename: %s`, handle.Filename)

	saveFile(w, file, handle)
}

func saveFile(w http.ResponseWriter, file multipart.File, handle *multipart.FileHeader) {
	data, err := ioutil.ReadAll(file)
	if err != nil {
		fmt.Fprintf(w, "%v", err)
		return
	}

	// NOTE: File extension includes the dot
	fileExtension := strings.ToLower(filepath.Ext(handle.Filename))
	if fileExtension == ".jpeg" {
		fileExtension = ".jpg"
	}

	filename := fmt.Sprintf(`%s%s`, createGUID(), fileExtension)

	folderPath := config.FilesPath
	filePath := filepath.Join(folderPath, filename)
	os.MkdirAll(folderPath, os.ModePerm)
	err = ioutil.WriteFile(filePath, data, 0666)
	if err != nil {
		jsonStringResponse(w, http.StatusInternalServerError, `{}`)
		return
	}
	url := fmt.Sprintf(`%s/files/%s`, config.ServerRoot, filename)
	log.Printf(`saveFile, url: %s`, url)
	json := fmt.Sprintf(`{ "url": "%s" }`, url)
	jsonStringResponse(w, http.StatusOK, json)
}

// Response helpers

func jsonStringResponse(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	fmt.Fprint(w, message)
}

func jsonBytesResponse(w http.ResponseWriter, code int, json []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(json)
}

func errorResponse(w http.ResponseWriter, code int, message string) {
	log.Printf("%d ERROR", code)
	w.WriteHeader(code)
	fmt.Fprint(w, message)
}

func addUserID(rw http.ResponseWriter, req *http.Request, next http.Handler) {
	ctx := context.WithValue(req.Context(), "userid", req.Header.Get("userid"))
	req = req.WithContext(ctx)
	next.ServeHTTP(rw, req)
}