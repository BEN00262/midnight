package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"unsafe"

	"github.com/getlantern/systray"
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

var (
	kernel32     = syscall.NewLazyDLL("kernel32.dll")
	createMutexW = kernel32.NewProc("CreateMutexW")
	getLastError = kernel32.NewProc("GetLastError")
)

func isAnotherInstanceRunning() bool {
	name := syscall.StringToUTF16Ptr("MidnightProxy")
	handle, _, err := createMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))

	if handle == 0 {
		log.Fatalf("Failed to create mutex: %v", err)
	}

	lastErr, _, _ := getLastError.Call()
	return lastErr == 183 // ERROR_ALREADY_EXISTS
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

func RunProxy(config Config) {
	// read the config.json file
	// this should be read from a manifest thats downloaded from a remote repository
	// this is to ensure that the plugin is always up to date

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

func SetupProxyAndRun(config Config) {
	// Disable proxy
	// defer EnableProxy(false, "", "")

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

	RunProxy(config)
}

func GenerateRegex(domain string) (string, error) {
	// Normalize the domain by trimming leading/trailing spaces and converting to lowercase
	domain = strings.ToLower(strings.TrimSpace(domain))

	// Validate the domain format using a basic regex
	re := regexp.MustCompile(`^(https?://)?([a-zA-Z0-9-]+\.)+[a-zA-Z]{2,6}$`)
	if !re.MatchString(domain) {
		return "", fmt.Errorf("invalid domain format")
	}

	// Split the domain by the '.' character to identify parts
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid domain format")
	}

	// Extract the second-level domain (assumes that the last part is the TLD)
	secondLevelDomain := parts[len(parts)-2]

	// Create the regex string, replacing the second-level domain placeholder
	regex := fmt.Sprintf(`^https?:\/\/([a-zA-Z0-9-]+\.)*%s\.[a-zA-Z]{2,6}\/?.*$`, secondLevelDomain)

	return regex, nil
}

func main() {
	// add flags to allow the user to specify the plugin path and domain
	// this will allow the user to specify the plugin path and domain to run the plugin on
	if isAnotherInstanceRunning() {
		// Send the new config to the running instance
		return
	}

	pluginPath := flag.String("plugin-path", "", "Path to the plugin file, should be a js file (required)")
	domainPattern := flag.String("domain-pattern", "", "Domain pattern to match (required)")

	flag.Parse()

	if *pluginPath == "" || *domainPattern == "" {
		fmt.Println("Both -plugin-path and -domain-pattern flags are required\nUsage:")

		// print the help
		flag.PrintDefaults()
		return
	}

	flag.Parse()

	// convert the plugin path to an absolute path
	abs_plugin_path, err := filepath.Abs(*pluginPath)

	if err != nil {
		log.Fatal(err)
	}

	// take the supplied domain and convert to this regex ^https?:\\/\\/([a-zA-Z0-9-]+\\.)*impactrooms\\.[a-zA-Z]{2,6}\\/?.*$
	domain_pattern_regex_string, err := GenerateRegex(*domainPattern)

	if err != nil {
		log.Fatal(err)
	}

	config := Config{
		Pattern:    domain_pattern_regex_string,
		PluginPath: abs_plugin_path,
	}

	go func(config Config) { SetupProxyAndRun(config) }(config)

	// Run the systray
	systray.Run(onReady, onExit)
}

func onReady() {
	// Set the icon (icon file must be in the same directory)
	iconData, err := assets.ReadFile("assets/icon.ico") // Use PNG or ICO file

	if err != nil {
		log.Fatal(err)
	}

	systray.SetIcon(iconData)
	systray.SetTitle("Midnight Proxy")

	// Set the tooltip
	systray.SetTooltip("Midnight Proxy")

	// Add menu items
	quitItem := systray.AddMenuItem("Stop Proxy", "Stop the proxy")

	// Handle menu item clicks in goroutines
	go func() {
		for range quitItem.ClickedCh {
			systray.Quit()
		}
	}()
}

func onExit() {
	EnableProxy(false, "", "")
}
