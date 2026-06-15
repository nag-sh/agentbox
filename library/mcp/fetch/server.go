package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
)

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      interface{}     `json:"id,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
	ID      interface{} `json:"id,omitempty"`
}

func main() {
	health := flag.Bool("health", false, "Run health check")
	flag.Parse()

	if *health {
		os.Exit(0)
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error reading stdin:", err)
			os.Exit(1)
		}

		var req JSONRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			sendError(nil, -32700, "Parse error")
			continue
		}

		handleRequest(req)
	}
}

func handleRequest(req JSONRPCRequest) {
	switch req.Method {
	case "initialize":
		res := map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"serverInfo": map[string]string{
				"name":    "fetch-server",
				"version": "1.0.0",
			},
		}
		sendResponse(req.ID, res)
	case "tools/list":
		res := map[string]interface{}{
			"tools": []map[string]interface{}{
				{
					"name":        "fetch",
					"description": "Fetch content from a URL",
					"inputSchema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"url": map[string]interface{}{
								"type":        "string",
								"description": "The URL to fetch",
							},
						},
						"required": []string{"url"},
					},
				},
			},
		}
		sendResponse(req.ID, res)
	case "tools/call":
		var params struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(req.ID, -32602, "Invalid params")
			return
		}
		if params.Name != "fetch" {
			sendError(req.ID, -32601, "Method not found")
			return
		}
		url, ok := params.Arguments["url"].(string)
		if !ok {
			sendError(req.ID, -32602, "Missing url parameter")
			return
		}

		resp, err := http.Get(url)
		if err != nil {
			sendCallResponse(req.ID, fmt.Sprintf("Error fetching URL: %v", err), true)
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			sendCallResponse(req.ID, fmt.Sprintf("Error reading body: %v", err), true)
			return
		}

		sendCallResponse(req.ID, string(body), false)

	default:
		sendResponse(req.ID, map[string]interface{}{})
	}
}

func sendResponse(id interface{}, result interface{}) {
	res := JSONRPCResponse{
		JSONRPC: "2.0",
		Result:  result,
		ID:      id,
	}
	data, _ := json.Marshal(res)
	fmt.Println(string(data))
}

func sendCallResponse(id interface{}, text string, isError bool) {
	res := map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": text,
			},
		},
		"isError": isError,
	}
	sendResponse(id, res)
}

func sendError(id interface{}, code int, message string) {
	res := JSONRPCResponse{
		JSONRPC: "2.0",
		Error: map[string]interface{}{
			"code":    code,
			"message": message,
		},
		ID: id,
	}
	data, _ := json.Marshal(res)
	fmt.Println(string(data))
}
