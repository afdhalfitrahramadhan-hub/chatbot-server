package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net/http"
    "os"
    "time"

    "github.com/gorilla/mux"
    "github.com/gorilla/websocket"
    "github.com/joho/godotenv"
)

// ===== STRUCT REQUEST/RESPONSE =====

type ChatRequest struct {
    Message string `json:"message"`
    UserID  string `json:"user_id"`
}

type ChatResponse struct {
    Reply     string `json:"reply"`
    Timestamp string `json:"timestamp"`
    Status    string `json:"status"`
}

// ===== STRUCT GROQ API =====

type GroqMessage struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}

type GroqRequest struct {
    Model    string        `json:"model"`
    Messages []GroqMessage `json:"messages"`
}

type GroqChoice struct {
    Message GroqMessage `json:"message"`
}

type GroqResponse struct {
    Choices []GroqChoice `json:"choices"`
    Error   *GroqError   `json:"error,omitempty"`
}

type GroqError struct {
    Message string `json:"message"`
}

// ===== WEBSOCKET UPGRADER =====

var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true
    },
}

// ===== FUNGSI PANGGIL GROQ API =====

func callGroqAPI(userMessage string) (string, error) {
    apiKey := os.Getenv("GROQ_API_KEY")
    model := os.Getenv("GROQ_MODEL")

    if model == "" {
        model = "llama3-8b-8192"
    }

    payload := GroqRequest{
        Model: model,
        Messages: []GroqMessage{
            {
                Role:    "system",
                Content: "Kamu adalah asisten chatbot yang ramah dan membantu. Jawab dalam bahasa Indonesia.",
            },
            {
                Role:    "user",
                Content: userMessage,
            },
        },
    }

    body, err := json.Marshal(payload)
    if err != nil {
        return "", fmt.Errorf("gagal marshal JSON: %v", err)
    }

    req, err := http.NewRequest("POST", "https://api.groq.com/openai/v1/chat/completions", bytes.NewBuffer(body))
    if err != nil {
        return "", fmt.Errorf("gagal buat request: %v", err)
    }

    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+apiKey)

    client := &http.Client{Timeout: 30 * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        return "", fmt.Errorf("gagal kirim request ke Groq: %v", err)
    }
    defer resp.Body.Close()

    respBody, err := io.ReadAll(resp.Body)
    if err != nil {
        return "", fmt.Errorf("gagal baca response: %v", err)
    }

    var groqResp GroqResponse
    if err := json.Unmarshal(respBody, &groqResp); err != nil {
        return "", fmt.Errorf("gagal parse response: %v", err)
    }

    // Cek error dari Groq
    if groqResp.Error != nil {
        return "", fmt.Errorf("Groq API error: %s", groqResp.Error.Message)
    }

    if len(groqResp.Choices) == 0 {
        return "", fmt.Errorf("tidak ada balasan dari Groq")
    }

    return groqResp.Choices[0].Message.Content, nil
}

// ===== HTTP HANDLER: POST /chat =====

func chatHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

    if r.Method == http.MethodOptions {
        w.WriteHeader(http.StatusOK)
        return
    }

    var req ChatRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        w.WriteHeader(http.StatusBadRequest)
        json.NewEncoder(w).Encode(ChatResponse{Status: "error", Reply: "Format JSON tidak valid"})
        return
    }

    if req.Message == "" {
        w.WriteHeader(http.StatusBadRequest)
        json.NewEncoder(w).Encode(ChatResponse{Status: "error", Reply: "Pesan tidak boleh kosong"})
        return
    }

    log.Printf("[HTTP] UserID: %s | Pesan: %s", req.UserID, req.Message)

    reply, err := callGroqAPI(req.Message)
    if err != nil {
        log.Printf("[HTTP] Error Groq: %v", err)
        w.WriteHeader(http.StatusInternalServerError)
        json.NewEncoder(w).Encode(ChatResponse{Status: "error", Reply: "Gagal mendapat respons dari AI"})
        return
    }

    log.Printf("[HTTP] Balasan Groq: %s", reply)

    json.NewEncoder(w).Encode(ChatResponse{
        Reply:     reply,
        Timestamp: time.Now().Format(time.RFC3339),
        Status:    "ok",
    })
}

// ===== WEBSOCKET HANDLER: GET /ws =====

func wsHandler(w http.ResponseWriter, r *http.Request) {
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Println("[WS] Gagal upgrade:", err)
        return
    }
    defer conn.Close()

    log.Println("[WS] Client terhubung:", r.RemoteAddr)

    for {
        var req ChatRequest
        if err := conn.ReadJSON(&req); err != nil {
            log.Println("[WS] Client disconnect:", err)
            break
        }

        log.Printf("[WS] Pesan dari %s: %s", req.UserID, req.Message)

        reply, err := callGroqAPI(req.Message)
        if err != nil {
            log.Printf("[WS] Error Groq: %v", err)
            conn.WriteJSON(ChatResponse{
                Status:    "error",
                Reply:     "Gagal mendapat respons dari AI",
                Timestamp: time.Now().Format(time.RFC3339),
            })
            continue
        }

        if err := conn.WriteJSON(ChatResponse{
            Reply:     reply,
            Timestamp: time.Now().Format(time.RFC3339),
            Status:    "ok",
        }); err != nil {
            log.Println("[WS] Gagal kirim:", err)
            break
        }
    }
}

// ===== HEALTH CHECK =====

func healthHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{
        "status":  "UP",
        "message": "Go Chatbot Server + Groq API berjalan normal",
        "model":   os.Getenv("GROQ_MODEL"),
    })
}

// ===== MAIN =====

func main() {
    // Load .env file
    if err := godotenv.Load(); err != nil {
        log.Println("File .env tidak ditemukan, menggunakan environment variable sistem")
    }

    // Validasi API Key
    if os.Getenv("GROQ_API_KEY") == "" {
        log.Fatal("❌ GROQ_API_KEY belum diset! Cek file .env")
    }

    port := os.Getenv("PORT")
    if port == "" {
        port = "8080"
    }

    r := mux.NewRouter()
    r.HandleFunc("/health", healthHandler).Methods("GET")
    r.HandleFunc("/chat", chatHandler).Methods("POST", "OPTIONS")
    r.HandleFunc("/ws", wsHandler)

    log.Printf("🚀 Server berjalan di port %s", port)
    log.Printf("   Model Groq : %s", os.Getenv("GROQ_MODEL"))
    log.Printf("   HTTP  → POST http://localhost:%s/chat", port)
    log.Printf("   WS    → ws://localhost:%s/ws", port)
    log.Printf("   Health→ GET  http://localhost:%s/health", port)

    if err := http.ListenAndServe(":"+port, r); err != nil {
        log.Fatal("Server error:", err)
    }
}
