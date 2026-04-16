package mesi

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func IsEsiResponse(response *http.Response) bool {
	header := strings.ToLower(response.Header.Get("Edge-control"))

	return strings.Contains(header, "dca=esi")
}

func isURLSafe(requestedURL string, config EsiParserConfig) error {
	if config.BlockPrivateIPs {
		parsedURL, err := url.Parse(requestedURL)
		if err != nil {
			return errors.New("invalid url: " + err.Error())
		}

		host := parsedURL.Host
		if host == "" {
			return errors.New("url has no host")
		}

		if len(config.AllowedHosts) > 0 {
			allowed := false
			for _, allowedHost := range config.AllowedHosts {
				if host == allowedHost || strings.HasSuffix(host, "."+allowedHost) {
					allowed = true
					break
				}
			}
			if !allowed {
				return errors.New("host not in allowed list: " + host)
			}
			return nil
		}

		ips, err := net.LookupIP(host)
		if err != nil {
			return errors.New("cannot resolve host: " + err.Error())
		}

		for _, ip := range ips {
			if isPrivateOrReservedIP(ip) {
				return errors.New("url resolves to private/reserved ip: " + ip.String())
			}
		}
	}

	return nil
}

func isPrivateOrReservedIP(ip net.IP) bool {
	privateBlocks := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"0.0.0.0/8",
		"224.0.0.0/4",
		"240.0.0.0/4",
	}

	for _, block := range privateBlocks {
		_, cidr, _ := net.ParseCIDR(block)
		if cidr.Contains(ip) {
			return true
		}
	}

	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
}

func singleFetchUrl(requestedURL string, config EsiParserConfig) (data string, esiResponse bool, err error) {
	if config.Timeout <= 0 {
		return "", false, errors.New("exceeded time budget")
	}

	if err := isURLSafe(requestedURL, config); err != nil {
		return "", false, errors.New("ssrf validation failed: " + err.Error())
	}

	client := http.Client{
		Timeout: config.Timeout,
	}

	url := requestedURL
	if !strings.HasPrefix(url, "http") && !strings.HasPrefix(url, "https") {
		if config.DefaultUrl == "" {
			return "", false, errors.New("default url can't be empty, on relative urls: " + url)
		}
		url = strings.TrimRight(config.DefaultUrl, "/") + "/" + strings.TrimLeft(url, "/")
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", false, errors.New("failed to create request: " + err.Error())
	}
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
