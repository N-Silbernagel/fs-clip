package main

import (
	"errors"
	"github.com/fsnotify/fsnotify"
	"golang.design/x/clipboard"
	"log"
	"net/http"
	"os"
	"strings"
)

func ensureDir(dirName string) error {
	err := os.Mkdir(dirName, 0777)
	if err == nil {
		return nil
	}
	if !os.IsExist(err) {
		return err
	}

	// check that the existing path is a directory
	info, err := os.Stat(dirName)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("path exists but is not a directory")
	}

	return nil
}

func addFileToClipboard(filePath string) error {
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	contentType := http.DetectContentType(fileContent)

	if strings.HasPrefix(contentType, "text/") ||
		strings.HasPrefix(contentType, "application/vnd.*") ||
		contentType == "application/rtf" ||
		contentType == "application/json" ||
		contentType == "application/xml" ||
		contentType == "application/pdf" {
		clipboard.Write(clipboard.FmtText, fileContent)
		return nil
	}

	if strings.HasPrefix(contentType, "image/") {
		clipboard.Write(clipboard.FmtImage, fileContent)
		return nil
	}

	return errors.New("unsupported fileContent type:" + contentType)
}

func main() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}

	watchPath := homeDir + string(os.PathSeparator) + "fs-clip-watch"

	err = ensureDir(watchPath)
	if err != nil {
		panic(err)
	}

	// Create new watcher.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		panic(err)
	}
	defer watcher.Close()

	err = clipboard.Init()
	if err != nil {
		panic(err)
	}

	go watchEvents(watcher)

	err = watcher.Add(watchPath)
	if err != nil {
		panic(err)
	}

	// Block main goroutine forever.
	<-make(chan struct{})
}

func watchEvents(watcher *fsnotify.Watcher) {
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			if event.Has(fsnotify.Create) {
				handleFileCreate(event)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Println("File watcher error:", err)
		}
	}
}

func handleFileCreate(event fsnotify.Event) {
	err := addFileToClipboard(event.Name)
	if err != nil {
		// fail silently, shouldn't interrupt the program
		log.Println("error while adding file to clipboard:", err)
		return
	}

	err = os.Remove(event.Name)
	if err != nil {
		// fail silently, shouldn't interrupt the program
		log.Println("error while removing file:", err)
	}
}
