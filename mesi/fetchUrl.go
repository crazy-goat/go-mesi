package mesi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

func singleFetchUrl(url string) (data string, err error) {
	client := http.Client{
		Timeout: 10 * time.Second,
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
