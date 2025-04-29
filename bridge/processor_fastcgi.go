package bridge

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"reflect"

	fcgiclient "github.com/tomasen/fcgi_client"
)

func NewFastCGIProcessor(net, addr, script string, log logger) Processor {
	return func(ctx context.Context, env map[string]string, body []byte) error {
		conn, err := fcgiclient.Dial(net, addr)
		if err != nil {
			log.Errorf("Unable to connect to FastCGI server: %v", err)
			return ErrProcessorInternal
		}

		if env == nil {
			env = map[string]string{}
		}

		if _, ok := env["REQUEST_METHOD"]; !ok {
			env["REQUEST_METHOD"] = "POST"
		}

		if _, ok := env["REQUEST_URI"]; !ok {
			env["REQUEST_URI"] = "/"
		}

		env["CONTENT_LENGTH"] = fmt.Sprint(len(body))
		env["SCRIPT_FILENAME"] = script

		resp, err := conn.Request(env, bytes.NewReader(append(body, 13, 10, 13, 10)))
		log.Debugf("FastCGI response: %v", resp)
		conn.Close()
		if err != nil {
			log.Errorf("An error occurred while making FastCGI request: %v", err)
			return ErrProcessorInternal
		}

		// Log all properties of the resp object
		log.Debugf("Logging all properties of the FastCGI response:")
		respValue := reflect.ValueOf(resp)
		if respValue.Kind() == reflect.Ptr {
			log.Debugf("Response is a pointer, dereferencing it")
			respValue = respValue.Elem() // Dereference the pointer
		}
		respType := respValue.Type()
		for i := 0; i < respType.NumField(); i++ {
			field := respType.Field(i)
			value := respValue.Field(i).Interface()

			// Special handling for the Body field
			if field.Name == "Body" {
				if bodyReader, ok := value.(io.ReadCloser); ok {
					bodyBytes, err := io.ReadAll(bodyReader)
					if err != nil {
						log.Errorf("Failed to read response body: %v", err)
					} else {
						log.Debugf("Body: %s", string(bodyBytes))
					}
					// Reset the body for further use
					respValue.Field(i).Set(reflect.ValueOf(io.NopCloser(bytes.NewReader(bodyBytes))))
					continue
				}
			}

			log.Debugf("%s: %v", field.Name, value)
		}

		c := resp.StatusCode / 100
		if c == 0 {
			return ErrUnknownStatus
		}

		if c == 2 {
			return nil
		}

		if c == 3 || c == 4 {
			return ErrProcessingError
		}

		return ErrProcessingFailed
	}
}
