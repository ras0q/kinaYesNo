package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/gif"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"strings"

	"github.com/traPtitech/go-traq"
	traqwsbot "github.com/traPtitech/traq-ws-bot"
	"github.com/traPtitech/traq-ws-bot/payload"
)

const apiURL = "https://yesno.wtf/api"

var (
	accessToken = os.Getenv("TRAQ_BOT_ACCESS_TOKEN")
	channelID   = os.Getenv("TRAQ_BOT_CHANNEL_ID")
)

type APIRes struct {
	Answer string
	Image  string
}

func main() {
	bot, err := traqwsbot.NewBot(&traqwsbot.Options{
		AccessToken: accessToken,
	})
	if err != nil {
		panic(err)
	}

	bot.OnError(func(message string) {
		log.Println("ERROR: bot.OnError:", message)
		return
	})

	bot.OnMessageCreated(func(p *payload.MessageCreated) {
		log.Println("INFO: bot.OnMessageCreated:", p.Message.PlainText)

		body, err := getBody(apiURL)
		if err != nil {
			log.Println("ERROR: getBody:", err)
			return
		}

		var apiRes APIRes
		if err := json.Unmarshal(body, &apiRes); err != nil {
			log.Println("ERROR: json.Unmarshal:", err)
		}

		body, err = getBody(apiRes.Image)
		if err != nil {
			log.Println("ERROR: getBody:", err)
			return
		}

		g, err := gif.DecodeAll(bytes.NewReader(body))
		if err != nil {
			log.Println("ERROR: gif.Decode:", err)
			return
		}

		file, err := os.Create("yesno.gif")
		if err != nil {
			log.Println("ERROR: os.Create:", err)
			return
		}
		defer file.Close()
		defer os.Remove(file.Name())

		if err := gif.EncodeAll(file, g); err != nil {
			log.Println("ERROR: gif.Encode:", err)
			return
		}

		file.Seek(0, io.SeekStart)

		fid, err := postFile(channelID, file)
		if err != nil {
			log.Println("ERROR: traqapi.PostFile:", err)
			return
		}

		if _, _, err := bot.API().MessageApi.
			PostMessage(context.Background(), channelID).
			PostMessageRequest(traq.PostMessageRequest{
				Content: fmt.Sprintf("%sやんね！\n\nhttps://q.trap.jp/files/%s", strings.Title(apiRes.Answer), fid),
			}).
			Execute(); err != nil {
			log.Println("ERROR: traqapi.PostMessage:", err)
			return
		}
	})

	log.Println("INFO: bot.Start")
	if err := bot.Start(); err != nil {
		panic(err)
	}
}

func getBody(url string) ([]byte, error) {
	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("invalid status: %d", res.StatusCode)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

func postFile(channelID string, file *os.File) (string, error) {
	// NOTE: go-traqがcontent-typeをapplication/octet-streamにしてしまうので自前でAPIを叩く
	// Ref: https://github.com/traPtitech/go-traq/blob/2c7a5f9aa48ef67a6bd6daf4018ca2dabbbbb2f3/client.go#L304
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)

	mh := make(textproto.MIMEHeader)
	mh.Set("Content-Type", "image/gif")
	mh.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, file.Name()))

	pw, err := mw.CreatePart(mh)
	if err != nil {
		return "", fmt.Errorf("failed to create part: %w", err)
	}

	if _, err := io.Copy(pw, file); err != nil {
		return "", fmt.Errorf("failed to copy file: %w", err)
	}

	contentType := mw.FormDataContentType()
	mw.Close()

	req, err := http.NewRequest(
		"POST",
		fmt.Sprintf("https://q.trap.jp/api/v3/files?channelId=%s", channelID),
		&b,
	)
	if err != nil {
		return "", fmt.Errorf("Error creating request: %w", err)
	}

	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := new(http.Client)

	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Error sending request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(res.Body)

		return "", fmt.Errorf("Error creating file: %s %s", res.Status, string(b))
	}

	var traqFile traq.FileInfo
	if err := json.NewDecoder(res.Body).Decode(&traqFile); err != nil {
		return "", fmt.Errorf("Error decoding response: %w", err)
	}

	return traqFile.Id, nil
}
