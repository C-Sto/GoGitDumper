package libgogitdumper

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
)

func GetThing(path string) ([]byte, error) {
	resp, err := http.Get(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, errors.New("404 File not found")
	} else if resp.StatusCode != 200 {
		return nil, errors.New(fmt.Sprintf("Error code: %d\n", resp.StatusCode))
	}

	buf := &bytes.Buffer{}
	buf.ReadFrom(resp.Body)
	body := buf.Bytes()

	return body, err
}
