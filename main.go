package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"

	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"

	"github.com/takama/daemon"
	"gopkg.in/elazarl/goproxy.v1"
)

const (
	SERVICE_NAME        = "VoryPayPluginServiceV0.0.1"
	SERVICE_dESCRIPTION = "A sample description to see"
)

// find a way to load an interceptor
type Config struct {
	Pattern         string `json:"pattern"`
	PluginPath      string `json:"path"`
	PluginName      string `json:"name"`
	PluginVersion   string `json:"version"`
	PluginSignature string `json:"signature"`
}

// VerifyRSASignature verifies the RSA signature using the public key
func VerifyRSASignature(message, signature []byte, publicKeyPEM string) bool {
	// Parse the public key
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil || block.Type != "PUBLIC KEY" {
		return false
	}

	pubKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return false
	}

	rsaPubKey, ok := pubKey.(*rsa.PublicKey)
	if !ok {
		return false
	}

	// Hash the message
	hash := sha256.New()
	hash.Write(message)
	hashed := hash.Sum(nil)

	// Verify the signature using PSS
	err = rsa.VerifyPSS(rsaPubKey, crypto.SHA256, hashed, signature, &rsa.PSSOptions{
		SaltLength: rsa.PSSSaltLengthAuto, // Automatically determine salt length
		Hash:       crypto.SHA256,
	})

	return err == nil
}

func RunProxy() {
	// read the config.json file
	config_file, err := os.ReadFile("config.json")

	if err != nil {
		log.Fatal(err)
	}

	// parse the config file
	var config Config
	err = json.Unmarshal(config_file, &config)

	if err != nil {
		log.Fatal(err)
	}

	// confirm that the signature of the script matches
	// script, err := os.ReadFile(config.PluginPath)

	// if err != nil {
	// 	log.Fatal(err)
	// }

	// signature, err := base64.StdEncoding.DecodeString(config.PluginSignature)

	// if err != nil {
	// 	log.Fatal("Error decoding signature:", err)
	// }

	// if !VerifyRSASignature(script, signature, `-----BEGIN PUBLIC KEY-----`) {
	// 	log.Fatal("Invalid plugin signature")
	// }

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

			// Execute the plugin
			fmt.Println("Executing plugin: ", config.PluginName, " version: ", config.PluginVersion, " signature: ", config.PluginSignature)

			cmd := exec.Command("deno", "run", config.PluginPath, req.Method, req.URL.String(), "request", string(unmarshed_json_post_data_string))

			if output, err := cmd.CombinedOutput(); err == nil {
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

			if (resp.Request.Method == "POST" || resp.Request.Method == "PUT" || resp.Request.Method == "PATCH" || resp.Request.Method == "DELETE") && resp.Request.ContentLength > 0 {
				buffer, err := io.ReadAll(resp.Body) // Reads the body

				if err != nil {
					return resp
				}

				// Unmarshal body into dynamic JSON (map[string]interface{})
				err = json.Unmarshal(buffer, &unmarshed_json_post_data)
				if err != nil {
					return resp
				}

				// IMPORTANT: Reset the body since ioutil.ReadAll consumes the body
				resp.Body = io.NopCloser(bytes.NewBuffer(buffer))
			}

			unmarshed_json_post_data_string, err := json.Marshal(unmarshed_json_post_data)

			if err != nil {
				return resp
			}

			// Execute the plugin
			fmt.Println("Executing plugin: ", config.PluginName, " version: ", config.PluginVersion, " signature: ", config.PluginSignature)

			cmd := exec.Command("deno", "run", config.PluginPath, resp.Request.Method, resp.Request.URL.String(), "response", string(unmarshed_json_post_data_string))

			if output, err := cmd.CombinedOutput(); err == nil {
				fmt.Println(string(output))
			}
		}

		return resp
	})

	log.Fatal(http.ListenAndServe(":8080", proxy))
}

func main() {
	srv, err := daemon.New(name, description, daemon.SystemDaemon, dependencies...)

	if err != nil {
		os.Exit(1)
	}

	service := &Service{srv}
	status, err := service.Manage()

	if err != nil {
		os.Exit(1)
	}

	fmt.Println(status)
}
