package main

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
)

func getCurrentDirectoryAbsolute() string {
	dir, err := os.Getwd()
	if err != nil {
		log.Fatalln(err.Error())
		return ""
	}
	return dir
}

func handleVersioned(ROOTDIR string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		v := mux.Vars(r)
		fileToOpen, ok := v["hash"]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		fileName, ok := v["fileName"]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		f := filepath.Clean(ROOTDIR + "/files/versioned/" + fileToOpen)
		b, err := os.ReadFile(f)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename="+fileName)
		w.Header().Set("Content-Length", fmt.Sprint(len(b)))
		w.Header().Set("Content-Transfer-Encoding", "binary")

		w.Write(b)
	}
}

func checkApiKey(key string) bool {
	allowedKeys := strings.Split(os.Getenv("API_KEYS"), ";")
	for _, k := range allowedKeys {
		if k == key {
			return true
		}
	}
	return false
}

//go:embed internal/upload.html
var UploadWebPage []byte

//go:embed internal/uploadReload.html
var UploadReloadWebPage []byte

func handleUpload(ROOTDIR string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method == "GET" {
			w.Header().Add("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			w.Write(UploadWebPage)
			return
		}

		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		defer func() {
			go runtime.GC()
		}()

		r.ParseMultipartForm(1024 * 1024 * 1024)
		file, handler, err := r.FormFile("file")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		versioned := r.FormValue("versioned")
		if versioned == "" {
			versioned = "off"
		}

		fileName := r.FormValue("fileName")
		if fileName == "" {
			fileName = handler.Filename
		}

		apikey := r.FormValue("apikey")
		if apikey == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if !checkApiKey(apikey) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		ffName := fileName

		ffName = strings.ReplaceAll(ffName, " ", "_")
		ffName = strings.ReplaceAll(ffName, "/", "_")
		ffName = strings.ReplaceAll(ffName, "\\", "_")
		ffName = filepath.Clean(ffName)

		finalLink := "/files/latest/" + ffName

		if versioned == "on" {
			buf := bytes.NewBuffer(nil)
			if _, err := io.Copy(buf, file); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			hash := sha256.Sum256(buf.Bytes())
			hashString := hex.EncodeToString(hash[:])
			f, err := os.Create(ROOTDIR + "/files/versioned/" + hashString)
			finalLink = "/files/versioned/" + hashString + "/" + ffName
			ffName = hashString
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			defer f.Close()
			_, err = io.Copy(f, buf)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		} else {
			f, err := os.Create(ROOTDIR + "/files/unversioned/" + ffName)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			defer f.Close()
			_, err = io.Copy(f, file)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf(string(UploadReloadWebPage), "<a href='"+finalLink+"'>"+ffName+"</a>")))
	}
}

//go:embed internal/index.html
var IndexPage []byte

func main() {
	PORT := os.Getenv("CDN_PORT")
	ROOTDIR := getCurrentDirectoryAbsolute()
	if PORT == "" {
		log.Fatalln("CDN_PORT environment variable is not set")
		return
	}

	r := mux.NewRouter()

	r.PathPrefix("/files/latest/").Handler(http.StripPrefix("/files/latest/", http.FileServer(http.Dir(ROOTDIR+"/files/unversioned/")))).Methods("GET", "OPTIONS")
	r.HandleFunc("/files/versioned/{hash}/{fileName}", handleVersioned(ROOTDIR)).Methods("GET", "OPTIONS")

	r.HandleFunc("/upload", handleUpload(ROOTDIR)).Methods("GET", "POST", "OPTIONS")
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write(IndexPage)
	}).Methods("GET", "OPTIONS")

	log.Println("Started server on port " + PORT)
	err := http.ListenAndServe(":"+PORT, handlers.LoggingHandler(os.Stdout, handlers.ProxyHeaders(r)))
	if err != nil {
		log.Fatalln(err.Error())
		return
	}
}
