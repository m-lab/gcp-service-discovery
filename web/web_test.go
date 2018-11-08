// web implements service discovery for generic HTTP or HTTPS URLs.
package web

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/m-lab/gcp-service-discovery/discovery"
)

func TestSource_Discover(t *testing.T) {

	tests := []struct {
		name        string
		target      string
		timeout     time.Duration
		fileContent string
		want        []discovery.StaticConfig
		badURL      string
		statusCode  int
		readAllFail bool
		wantErr     bool
	}{
		{
			name: "success",
			fileContent: `
			[
				{
					"targets": ["okay"],
					"labels": {"a":"b"}
				}
			]`,
			statusCode: http.StatusOK,
			want: []discovery.StaticConfig{
				{
					Targets: []string{"okay"},
					Labels:  map[string]string{"a": "b"},
				},
			},
		},
		{
			name:    "failure-bad-url",
			badURL:  "http://badurl:100",
			wantErr: true,
		},
		{
			name:       "failure-bad-http-status",
			statusCode: http.StatusNotFound,
			wantErr:    true,
		},
		{
			name:       "failure-bad-file-content",
			statusCode: http.StatusOK,
			wantErr:    true,
		},
		{
			name:        "failure-readall-fails",
			statusCode:  http.StatusOK,
			readAllFail: true,
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.statusCode)
					fmt.Fprintln(w, tt.fileContent)
				}),
			)
			defer ts.Close()

			url := ts.URL
			if tt.badURL != "" {
				url = tt.badURL
			}
			if tt.readAllFail {
				readAll = func(r io.Reader) ([]byte, error) {
					return nil, fmt.Errorf("Fake Read Error")
				}
			} else {
				readAll = ioutil.ReadAll
			}
			srv := NewService(url, 5*time.Second)
			got, err := srv.Discover(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("Source.Discover() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Source.Discover() = %v, want %v", got, tt.want)
			}
		})
	}
}
