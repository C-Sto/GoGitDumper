package libgogitdumper

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
)

func GetThing(path string, client *http.Client) ([]byte, error) {
	request, err := http.NewRequest("GET", path, nil)
	resp, err := client.Do(request)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, errors.New("404 File not found")
	} else if resp.StatusCode != 200 {
		return nil, errors.New(fmt.Sprintf("Error code: %d\n", resp.StatusCode))
	}

	body, e := ioutil.ReadAll(resp.Body)
	if e != nil {
		return body, err
	}

	return body, err
}
