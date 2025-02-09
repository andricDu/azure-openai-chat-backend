package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
)

type ChatRequest struct {
	Message string `json:"message"`
}

type Reference struct {
	Source     string `json:"source"`
	Title      string `json:"title"`
	Authors    string `json:"authors,omitempty"`
	Year       string `json:"year,omitempty"`
	URL        string `json:"url,omitempty"`
	AccessDate string `json:"accessDate,omitempty"`
}

type EnhancedChatResponse struct {
	Response   string      `json:"response"`
	References []Reference `json:"references"`
	MainPoints []string    `json:"mainPoints,omitempty"`
}

type ChatResponse struct {
	Response   string   `json:"response"`
	References []string `json:"references,omitempty"`
}

type ChatChoice struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Index        int    `json:"index"`
	FinishReason string `json:"finish_reason"`
}

type AzureResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
}

// Helper function to format the prompt
func formatPromptWithReferenceRequest(message string) string {
	return fmt.Sprintf(`%s

Please provide a detailed response with references. Include:
1. A clear explanation
2. Supporting evidence
3. Specific citations
4. A numbered list of references at the end

Format references using a standard academic format.`, message)
}

// Parse the response to separate content and references
func parseResponseAndReferences(content string) (string, []string) {
	parts := strings.Split(content, "References:")
	if len(parts) < 2 {
		return content, nil
	}

	mainContent := strings.TrimSpace(parts[0])
	referencesText := strings.TrimSpace(parts[1])

	// Parse references into a slice
	var references []string
	for _, line := range strings.Split(referencesText, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			references = append(references, trimmed)
		}
	}

	return mainContent, references
}

func chatHandler(w http.ResponseWriter, r *http.Request) {
	var chatRequest ChatRequest
	err := json.NewDecoder(r.Body).Decode(&chatRequest)
	if err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	apiKey := os.Getenv("AZURE_API_KEY")
	endpoint := os.Getenv("AZURE_ENDPOINT")

	data := map[string]interface{}{
		"messages": []map[string]interface{}{
			{
				"role": "system",
				"content": `You are a helpful assistant that provides detailed, accurate information with references.
            When providing information:
            1. Include relevant citations and sources
            2. Use a consistent citation format
            3. List all references at the end of your response
            4. Prefer academic sources, official documentation, and reliable websites
            5. Format your response as follows:
                - Main answer
                - Supporting details
                - References (numbered list)`,
			},
			{
				"role":    "user",
				"content": formatPromptWithReferenceRequest(chatRequest.Message),
			},
		},
		"data_sources": []map[string]interface{}{ // Changed from extra_body to dataSources
			{
				"type": "azure_search",
				"parameters": map[string]interface{}{
					"endpoint":               os.Getenv("AZURE_SEARCH_ENDPOINT"),
					"key":                    os.Getenv("AZURE_SEARCH_KEY"),
					"index_name":             os.Getenv("AZURE_SEARCH_INDEX"),
					"query_type":             "simple",
					"semantic_configuration": "default",
					"role_information":       "You are an AI assistant that helps people with questions using the provided documentation.",
					"filter":                 nil,
					"strictness":             3,
					"authentication": map[string]interface{}{
						"type": "api_key",
						"key":  os.Getenv("AZURE_SEARCH_KEY"),
					},
				},
			},
		},
		"max_tokens":        2000,
		"temperature":       0.9,
		"top_p":             0.95,
		"frequency_penalty": 0.5,
		"presence_penalty":  0.5,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		http.Error(w, "Failed to marshal request data", http.StatusInternalServerError)
		return
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-key", apiKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Failed to send request to Azure OpenAI", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Failed to read response from Azure OpenAI", http.StatusInternalServerError)
		return
	}

	log.Printf("Raw response from Azure: %s", string(body))

	var azureResponse AzureResponse
	err = json.Unmarshal(body, &azureResponse)
	if err != nil {
		log.Printf("Unmarshal error: %v", err)
		http.Error(w, "Failed to unmarshal response data", http.StatusInternalServerError)
		return
	}

	if len(azureResponse.Choices) == 0 {
		http.Error(w, "No response choices returned", http.StatusInternalServerError)
		return
	}

	responseContent := azureResponse.Choices[0].Message.Content
	mainContent, references := parseResponseAndReferences(responseContent)

	chatResponse := ChatResponse{
		Response:   mainContent,
		References: references,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(chatResponse)
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	r := mux.NewRouter()
	r.HandleFunc("/api/chat", chatHandler).Methods("POST")

	log.Println("Server started at :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}
