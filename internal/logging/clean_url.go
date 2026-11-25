package logging

import (
	"errors"
	"net/url"
	"reflect"
)

func CleanURL(value string) (ret string) {
	u, err := url.Parse(value)
	if err != nil {
		return
	}
	return CleanURLValue(u).String()
}

func CleanURLValue(value *url.URL) *url.URL {
	u := *value
	u.User = nil
	u.ForceQuery = false
	u.RawQuery = ""
	u.Fragment = ""
	u.RawFragment = ""
	return &u
}

func CleanURLError(err error) error {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) || reflect.TypeOf(err) != reflect.TypeOf(urlErr) {
		return err
	}

	cleaned := *urlErr
	cleaned.URL = CleanURL(urlErr.URL)
	cleaned.Err = CleanURLError(urlErr.Err)
	return &cleaned
}
