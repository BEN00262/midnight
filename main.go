package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
	"gopkg.in/elazarl/goproxy.v1"
)

//go:embed assets/*
var assets embed.FS

type Config struct {
	Pattern         string `json:"pattern"`
	PluginPath      string `json:"path"`
	PluginName      string `json:"name"`
	PluginVersion   string `json:"version"`
	PluginSignature string `json:"signature"`
}

func ExtractResponse(input, marker string) (string, error) {
	start := strings.Index(input, marker)
	if start == -1 {
		return "", fmt.Errorf("no %s found", marker)
	}

	// Find the starting point of the JSON body
	start += len(marker)
	stack := 1
	body := strings.Builder{}
	body.WriteByte('{') // Append the opening curly brace

	for i := start; i < len(input); i++ {
		char := input[i]
		if char == '{' {
			stack++
		} else if char == '}' {
			stack--
		}

		// Append character to the buffer
		body.WriteByte(char)

		// Stop when all braces are matched
		if stack == 0 {
			return body.String(), nil
		}
	}

	return "", fmt.Errorf("unmatched braces in input")
}

func ConvertToValidJSON(input string) string {
	// Regex to find unquoted keys
	re := regexp.MustCompile(`([a-zA-Z0-9_]+):`)

	// Replace unquoted keys with quoted keys
	output := re.ReplaceAllString(input, `"$1":`)

	// Remove extra spaces (optional)
	output = strings.TrimSpace(output)

	return output
}

func RunProxy() {
	// read the config.json file
	// this should be read from a manifest thats downloaded from a remote repository
	// this is to ensure that the plugin is always up to date

	config_file, err := assets.ReadFile("assets/config.json")

	if err != nil {
		log.Fatal(err)
	}

	// parse the config file
	var config Config
	err = json.Unmarshal(config_file, &config)

	if err != nil {
		log.Fatal(err)
	}

	setCA()

	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = false

	proxy.OnRequest().
		HandleConnect(goproxy.AlwaysMitm)

	proxy.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		// Read the body content

		re := regexp.MustCompile(config.Pattern)

		// Check if the request URL matches the impactrooms domain
		if re.MatchString(req.URL.String()) {
			var unmarshed_json_post_data map[string]interface{}

			if (req.Method == "POST" || req.Method == "PUT" || req.Method == "PATCH" || req.Method == "DELETE") && req.ContentLength > 0 {
				buffer, err := io.ReadAll(req.Body) // Reads the body

				if err != nil {
					return req, nil
				}

				// Unmarshal body into dynamic JSON (map[string]interface{})
				err = json.Unmarshal(buffer, &unmarshed_json_post_data)
				if err != nil {
					return req, nil
				}

				// IMPORTANT: Reset the body since ioutil.ReadAll consumes the body
				req.Body = io.NopCloser(bytes.NewBuffer(buffer))
			}

			unmarshed_json_post_data_string, err := json.Marshal(unmarshed_json_post_data)

			if err != nil {
				return req, nil
			}

			// read the plugin file
			// this means we can pull this from a remote repository - run logic on whose scripts are run there

			plugin_content, err := ioutil.ReadFile(config.PluginPath)

			if err != nil {
				return req, nil
			}

			// only allowed perms is the network access
			cmd := exec.Command("deno", "eval", string(plugin_content), req.Method, req.URL.String(), "request", string(unmarshed_json_post_data_string))

			if output, err := cmd.CombinedOutput(); err == nil {
				body_detector := regexp.MustCompile(`@BODY\s*{\s*([^}]*)\s*}`)

				// Find the match
				body_matches := body_detector.FindStringSubmatch(string(output))

				if len(body_matches) > 0 {
					raw_body := body_matches[1]

					re := regexp.MustCompile(`\s*([a-zA-Z0-9_]+):\s*"([^"]+)"`)

					// Find all matches
					matches := re.FindAllStringSubmatch(raw_body, -1)

					// Create a map to store the dynamic structure
					raw_json_data := make(map[string]string)

					// Iterate through matches to extract keys
					for _, match := range matches {
						if len(match) > 1 {
							raw_json_data[match[1]] = match[2]
						}
					}

					if modified_json_body, err := json.Marshal(raw_json_data); err == nil {
						modified_json_body_reader := bytes.NewReader(modified_json_body)

						buffer := new(bytes.Buffer)

						if _, err = buffer.ReadFrom(modified_json_body_reader); err == nil {

							req.Body = io.NopCloser(buffer)
							req.ContentLength = int64(buffer.Len())
						}
					}
				}

				fmt.Println(string(output))
			}
		}

		return req, nil
	})

	proxy.OnResponse().DoFunc(func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
		// Read the body content

		re := regexp.MustCompile(config.Pattern)

		// Check if the request URL matches the impactrooms domain
		if re.MatchString(resp.Request.URL.String()) {
			var unmarshed_json_post_data map[string]interface{}

			if ((resp.Request.Method == "POST" || resp.Request.Method == "PUT" || resp.Request.Method == "PATCH" || resp.Request.Method == "DELETE") && resp.Request.ContentLength > 0) || resp.Request.Method == "GET" {
				buffer, err := io.ReadAll(resp.Body) // Reads the body

				if err != nil {
					return resp
				}

				err = json.Unmarshal(buffer, &unmarshed_json_post_data)

				if err != nil {
					return resp
				}

				resp.Body = io.NopCloser(bytes.NewBuffer(buffer))
			}

			unmarshed_json_post_data_string, err := json.Marshal(unmarshed_json_post_data)

			if err != nil {
				return resp
			}

			plugin_content, err := ioutil.ReadFile(config.PluginPath)

			if err != nil {
				return resp
			}

			cmd := exec.Command("deno", "eval", string(plugin_content), resp.Request.Method, resp.Request.URL.String(), "response", string(unmarshed_json_post_data_string))

			if output, err := cmd.CombinedOutput(); err == nil {
				extracted_response, err := ExtractResponse(string(output), "@RESPONSE {")

				if err != nil {
					return resp
				}

				raw_body := ConvertToValidJSON(extracted_response)

				raw_json_data, ok := gjson.Parse(raw_body).Value().(map[string]interface{})

				if !ok {
					return resp
				}

				if modified_json_body, err := json.Marshal(raw_json_data); err == nil {
					modified_json_body_reader := bytes.NewReader(modified_json_body)

					buffer := new(bytes.Buffer)

					if _, err = buffer.ReadFrom(modified_json_body_reader); err == nil {

						resp.Body = io.NopCloser(buffer)
						resp.ContentLength = int64(buffer.Len())
					}
				}

				fmt.Println(string(output))
			}
		}

		return resp
	})

	log.Fatal(http.ListenAndServe(":8888", proxy))
}

func main() {
	// Disable proxy
	defer EnableProxy(false, "", "")

	// gather deno -- used to execute the plugins
	install_deno_runtime()

	caCert, err := assets.ReadFile("assets/ca_cert.pem")

	if err != nil {
		log.Fatal(err)
	}

	tempFile, err := os.CreateTemp("", "ca_cert-*.pem")

	if err != nil {
		log.Fatal(err)
	}

	defer os.Remove(tempFile.Name()) // Clean up after use

	if _, err = tempFile.Write(caCert); err != nil {
		log.Fatal(err)
	}

	if err = AddCertToStore(tempFile.Name(), "ROOT", CERT_SYSTEM_STORE_CURRENT_USER); err != nil {
		log.Fatal(err)
	}

	// Proxy settings
	proxyAddress := "http://127.0.0.1:8888" // Replace with your proxy address
	bypassList := "localhost;127.0.0.1"     // Optional bypass list

	// Enable proxy
	if err = EnableProxy(true, proxyAddress, bypassList); err != nil {
		fmt.Printf("Error setting proxy: %v\n", err)
	}

	RunProxy()
}
