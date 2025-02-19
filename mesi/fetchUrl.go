package mesi

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func singleFetchUrl(url string, config EsiParserConfig) (data string, err error) {
	if config.timeout <= 0 {
		return "", errors.New("exceeded time budget")
	}

	client := http.Client{
		Timeout: config.timeout,
	}

	if !strings.HasPrefix(url, "http") && !strings.HasPrefix(url, "https") {
		if config.defaultUrl == "" {
			return "", errors.New("default url can't be empty, on relative urls: " + url)
		}
		url = strings.TrimRight(config.defaultUrl, "/") + "/" + strings.TrimLeft(url, "/")
	}

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Surrogate-Capability", "ESI/1.0")

	content, err := client.Do(req)
	if err != nil {
		return "", err
	} else {
		data, err := io.ReadAll(content.Body)
		if err != nil {
			return "", err
		}

		if content.StatusCode >= 400 {
			return "", errors.New(strconv.Itoa(content.StatusCode) + ": " + string(data))
		}
		return string(data), nil
	}
}
