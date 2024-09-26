package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os/exec"
	"regexp"

	"gopkg.in/elazarl/goproxy.v1"
)

// find a way to load an interceptor
type Config struct {
	Pattern    string `json:"pattern"`
	PluginPath string `json:"plugin_path"`
}

func main() {
	// read the config.json file
	config_file, err := ioutil.ReadFile("config.json")

	if err != nil {
		log.Fatal(err)
	}

	// parse the config file
	var config Config
	err = json.Unmarshal(config_file, &config)

	if err != nil {
		log.Fatal(err)
	}

	setCA(caCert, caKey)

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
				buffer, err := ioutil.ReadAll(req.Body) // Reads the body

				if err != nil {
					return req, nil
				}

				// Unmarshal body into dynamic JSON (map[string]interface{})
				err = json.Unmarshal(buffer, &unmarshed_json_post_data)
				if err != nil {
					return req, nil
				}

				// IMPORTANT: Reset the body since ioutil.ReadAll consumes the body
				req.Body = ioutil.NopCloser(bytes.NewBuffer(buffer))
			}

			unmarshed_json_post_data_string, err := json.Marshal(unmarshed_json_post_data)

			if err != nil {
				return req, nil
			}

			cmd := exec.Command("deno", "run", config.PluginPath, req.Method, req.URL.String(), string(unmarshed_json_post_data_string))

			if output, err := cmd.CombinedOutput(); err == nil {
				fmt.Println(string(output))
			}
		}

		return req, nil
	})

	log.Fatal(http.ListenAndServe(":8080", proxy))
}
