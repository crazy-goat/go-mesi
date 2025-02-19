package mesi

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func IsEsiResponse(response *http.Response) bool {
	header := strings.ToLower(response.Header.Get("Edge-control"))

	return strings.Contains(header, "dca=esi")
}

func singleFetchUrl(url string, config EsiParserConfig) (data string, esiResponse bool, err error) {
	if config.Timeout <= 0 {
		return "", false, errors.New("exceeded time budget")
	}

	client := http.Client{
		Timeout: config.Timeout,
	}

	if !strings.HasPrefix(url, "http") && !strings.HasPrefix(url, "https") {
		if config.DefaultUrl == "" {
			return "", false, errors.New("default url can't be empty, on relative urls: " + url)
		}
		url = strings.TrimRight(config.DefaultUrl, "/") + "/" + strings.TrimLeft(url, "/")
	}

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Surrogate-Capability", "ESI/1.0")

	content, err := client.Do(req)
	if err != nil {
		return "", false, err
	} else {
		data, err := io.ReadAll(content.Body)
		if err != nil {
			return "", false, err
		}

		if content.StatusCode >= 400 {
			return "", false, errors.New(strconv.Itoa(content.StatusCode) + ": " + string(data))
		}
		return string(data), IsEsiResponse(content), nil
	}
}
