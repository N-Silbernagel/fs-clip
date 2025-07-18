package main

import (
	"errors"
	"github.com/fsnotify/fsnotify"
	"golang.design/x/clipboard"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
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

	if len(fileContent) == 0 {
		return errors.New("file content is empty")
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
	// mutex to avoid concurrently writing to timers
	mu := sync.Mutex{}
	// timers to keep track of the files we are "manging" and want to write to clipboard once no more writes happen
	timers := make(map[string]*time.Timer)

	timerWait := 100 * time.Millisecond

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			fileName := event.Name

			if event.Has(fsnotify.Create) {
				// creating a new file in the watched folder means it will be "managed" by fs-clip
				// we create a timer to copy the file to clipboard once no more writes happen
				writeToClipboardTimer := time.AfterFunc(timerWait, func() {
					err := addFileToClipboard(fileName)
					if err != nil {
						// fail silently, shouldn't interrupt the program
						log.Println("error while adding file to clipboard:", err)
						return
					}

					err = os.Remove(fileName)
					if err != nil {
						// fail silently, shouldn't interrupt the program
						log.Println("error while removing file:", err)
					}

					mu.Lock()
					delete(timers, fileName)
					mu.Unlock()
				})

				mu.Lock()
				timers[fileName] = writeToClipboardTimer
				mu.Unlock()
			}

			if event.Has(fsnotify.Write) {
				// on file write we reset the timer, if now more writes happen to the file we can happily write it to
				// the clipboard
				mu.Lock()
				timer, ok := timers[fileName]
				mu.Unlock()

				// if no timer exists for the file, we treat it as not managed by fs-clip
				if !ok {
					return
				}

				timer.Reset(timerWait)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Println("File watcher error:", err)
		}
	}
}
