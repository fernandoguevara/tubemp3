package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/kkdai/youtube/v2"
	"golang.design/x/clipboard"
)

const (
	configFile    = "config.json"
	videoUrl      = "https://www.youtube.com/watch?v="
	shortVideoUrl = "https://youtu.be/"
	playlistUrl   = "https://www.youtube.com/playlist?list="
)

var (
	maxDownloads = 5
	logPath      = "./log.log"
	downloadPath = "."
	client       = youtube.Client{}
	workers      = make(chan struct{}, maxDownloads)
	mw           io.Writer
)

type Configuration struct {
	MaxDownloads int
	LogPath      string
	DownloadPath string
}

func main() {

	if exists, err := exists(configFile); exists {
		readConfigFile()
		if err != nil {
			log.Println(err.Error())
		}
	} else {
		log.Println("config.json not found, using default values...")
	}

	file := startLogging()
	defer file.Close()

	log.Println("tubemp3 is running...")
	log.Println("start copying(CTRL + C) your youtube videos and playlists")

	clipboardWatcher()

}

func readConfigFile() {
	file, _ := os.Open(configFile)
	defer file.Close()
	decoder := json.NewDecoder(file)
	configuration := Configuration{}
	err := decoder.Decode(&configuration)

	log.Println("Found config.json file!")
	log.Println(configuration)

	maxDownloads = configuration.MaxDownloads
	logPath = configuration.LogPath
	downloadPath = configuration.DownloadPath
	workers = make(chan struct{}, maxDownloads)

	if err != nil {
		log.Println(err.Error())
	}
}

func startLogging() *os.File {
	file, err := os.OpenFile(logPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Println(err.Error())
	}
	multi := io.MultiWriter(file, os.Stdout)
	log.SetOutput(multi)
	return file
}

func clipboardWatcher() {
	ch := clipboard.Watch(context.TODO(), clipboard.FmtText)
	for data := range ch {
		if isYoutubeResource(string(data)) {
			err := download(string(data))
			if err != nil {
				log.Println(err.Error())
			}
		}
	}
}

func isYoutubeResource(text string) bool {
	return strings.Contains(text, "youtube.com")
}

func download(url string) error {

	id, isPlaylist := getYoutubeId(url)

	if len(id) == 0 {
		return fmt.Errorf("No valid youtube URL detected")
	}

	if isPlaylist {
		go downloadPlaylist(url)
	} else {
		video, err := client.GetVideo(url)
		if err != nil {
			log.Println(err.Error())
		}

		go downloadAudio(video, "", nil)
	}

	return nil
}

func downloadPlaylist(url string) {

	playlist, err := client.GetPlaylist(url)
	if err != nil {
		log.Println(err.Error())
	}

	playlistTitle := sanitize(playlist.Title)
	folderPath := fmt.Sprintf("%s/%s/", downloadPath, playlistTitle)
	os.Mkdir(folderPath, 0755)

	playlistMessage := fmt.Sprintf("%s by %s", playlistTitle, playlist.Author)
	if len(playlist.Author) < 1 {
		playlistMessage = playlistTitle
	}
	log.Printf("Downloading Playlist %s\n", playlistMessage)

	var wg sync.WaitGroup
	for _, entry := range playlist.Videos {
		wg.Add(1)
		video, err := client.VideoFromPlaylistEntry(entry)
		if err != nil {
			log.Println(err.Error())
		}
		go downloadAudio(video, folderPath, &wg)
	}
	wg.Wait()
	log.Printf("Playlist Finished %s\n", playlistMessage)
}

func downloadAudio(video *youtube.Video, filePath string, wg *sync.WaitGroup) {
	defer func() {
		<-workers

		if wg != nil {
			wg.Done()
		}
	}()

	workers <- struct{}{}

	videoTitle := sanitize(video.Title)
	videoMessage := fmt.Sprintf("%s by '%s'!\n", videoTitle, video.Author)
	log.Printf("Downloading %s", videoMessage)

	videoFormat := getAudioFormat(video)
	stream, _, err := client.GetStream(video, &videoFormat)
	if err != nil {
		log.Println(err.Error())
	}

	fileName := fmt.Sprintf("%s%s.mp3", filePath, videoTitle)
	file, err := os.Create(fileName)
	if err != nil {
		log.Println(err.Error())
	}

	defer file.Close()
	_, err = io.Copy(file, stream)

	log.Printf("Finished %s", videoMessage)
}

func getAudioFormat(video *youtube.Video) youtube.Format {
	for _, format := range video.Formats {

		if format.AudioChannels > 0 &&
			strings.Contains(format.MimeType, "audio/mp4") {
			return format
		}
	}

	return video.Formats[0]
}

func getYoutubeId(url string) (id string, isPlaylist bool) {

	if strings.Contains(url, videoUrl) {
		return strings.Split(url, "watch?v=")[1], false
	} else if strings.Contains(url, shortVideoUrl) {
		return strings.Split(url, "be/")[1], false
	} else if strings.Contains(url, playlistUrl) {
		return strings.Split(url, "playlist?list=")[1], true
	} else {
		return "", false
	}
}

func sanitize(text string) string {

	re, err := regexp.Compile(`[^\w]`)
	if err != nil {
		log.Println(err.Error())
	}

	text = re.ReplaceAllString(text, " ")

	re, err = regexp.Compile(`\s+`)
	if err != nil {
		log.Println(err.Error())
	}

	return re.ReplaceAllString(text, " ")
}

func exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
