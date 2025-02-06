package mesi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func singleFetchUrl(url string, defaultUrl string) (data string, err error) {
	client := http.Client{
		Timeout: 10 * time.Second,
	}

	if !strings.HasPrefix(url, "http") && !strings.HasPrefix(url, "https") {
		if defaultUrl == "" {
			return "", errors.New("default url can't be empty, on relative urls: " + url)
		}
		url = defaultUrl + url
	}

	content, err := client.Get(url)
	if err != nil {
		fmt.Println("error fetching url")
		return "", err
	} else {
		data, err := io.ReadAll(content.Body)
		if err != nil {
			fmt.Println("error reading body")
			return "", err
		}

		if content.StatusCode >= 400 {
			return "", errors.New(strconv.Itoa(content.StatusCode) + ": " + string(data))
		}
		return string(data), nil
	}
}
