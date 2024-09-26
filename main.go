package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"regexp"

	"github.com/robertkrimen/otto"
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

	vm := otto.New()

	// Load JavaScript code from a file
	scriptContent, err := ioutil.ReadFile(config.PluginPath)

	if err != nil {
		log.Fatal(err)
	}

	_, err = vm.Run(string(scriptContent))

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
			var jsonData map[string]interface{}

			if req.Method == "POST" && req.ContentLength > 0 {
				buffer, err := ioutil.ReadAll(req.Body) // Reads the body
				if err != nil {
					return req, nil
				}

				// Unmarshal body into dynamic JSON (map[string]interface{})
				err = json.Unmarshal(buffer, &jsonData)
				if err != nil {
					return req, nil
				}

				// Print the dynamic JSON object
				fmt.Printf("Parsed JSON: %s %s %#v\n", req.Method, req.URL.String(), jsonData)

				// IMPORTANT: Reset the body since ioutil.ReadAll consumes the body
				req.Body = ioutil.NopCloser(bytes.NewBuffer(buffer))
			}

			// call the script using otto and pass the data through
			vm.Call("voryposplugin_handler", nil, req.Method, req.URL.String(), jsonData)
		}

		return req, nil
	})

	log.Fatal(http.ListenAndServe(":8080", proxy))
}
