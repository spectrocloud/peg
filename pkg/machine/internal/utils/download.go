package utils

import (
	"net/url"
	"time"

	"github.com/cavaliergopher/grab/v3"
	logging "github.com/ipfs/go-log"
)

var log = logging.Logger("download")

func IsValidURL(toTest string) bool {
	_, err := url.ParseRequestURI(toTest)
	if err != nil {
		return false
	}

	u, err := url.Parse(toTest)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}

	return true
}

func Download(url, dest string) error {
	client := grab.NewClient()
	req, err := grab.NewRequest(dest, url)
	if err != nil {
		return err
	}
	// start download

	log.Info("Downloading %v...", req.URL())
	resp := client.Do(req)
	log.Debugf("  %v", resp.HTTPResponse.Status)

	// start UI loop
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()

Loop:
	for {
		select {
		case <-t.C:
			log.Debugf("  transferred %v / %v bytes (%.2f%%)",
				resp.BytesComplete(),
				resp.Size,
				100*resp.Progress())

		case <-resp.Done:
			// download is complete
			break Loop
		}
	}

	// check for errors
	if err := resp.Err(); err != nil {
		return err
	}

	log.Infof("Download saved to .%v ", resp.Filename)

	return nil
}
