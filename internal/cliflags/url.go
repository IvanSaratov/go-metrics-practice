package cliflags

import "net/url"

// NormalizeBaseURL оставляет адрес со схемой и хостом без изменений.
// В остальных случаях добавляет http:// в начало адреса.
func NormalizeBaseURL(address string) string {
	parsedURL, err := url.Parse(address)
	if err == nil && parsedURL.Scheme != "" && parsedURL.Host != "" {
		return address
	}

	return "http://" + address
}
