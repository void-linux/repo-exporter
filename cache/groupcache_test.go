package cache

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestGet(t *testing.T) {
	storage := NewRepoDataStorage("repodata", 64<<20, "http://localhost:8080", getBytes)
	var tests = []struct {
		name                string
		givenKey            string
		expectedData        []byte
		expectedTimeElapsed time.Duration
		expectedErr         error
	}{
		{
			"Storing slow data for the first time with success",
			"test",
			[]byte("success"),
			101 * time.Millisecond,
			nil,
		},
		{
			"Getting cached data with success",
			"test",
			[]byte("success"),
			10 * time.Millisecond,
			nil,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			var response []byte
			start := time.Now()
			err := storage.Get(context.Background(), tt.givenKey, &response)
			finished := time.Since(start)
			if err != tt.expectedErr {
				t.Errorf("(%s): expected error %s, actual %s", tt.givenKey, tt.expectedErr, err)
			}

			if finished > tt.expectedTimeElapsed {
				t.Errorf("(%s): expected %s, actual %s", tt.givenKey, tt.expectedTimeElapsed, finished)
			}

			if bytes.Compare(response, tt.expectedData) != 0 {
				t.Errorf("(%s): expected %s, actual %s", tt.givenKey, tt.expectedData, response)
			}

		})
	}
}

func getBytes(_ string) ([]byte, error) {
	time.Sleep(100 * time.Millisecond)
	return []byte("success"), nil
}
