package helpers

import "net/url"

func NormalizeBaseURL(address string) string {
	parsedURL, err := url.Parse(address)
	if err == nil && parsedURL.Scheme != "" && parsedURL.Host != "" {
		return address
	}

	return "http://" + address
}
