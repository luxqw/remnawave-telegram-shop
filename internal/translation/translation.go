package translation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-telegram/bot/models"
)

type ButtonData struct {
	Text    string `json:"text"`
	Style   string `json:"style,omitempty"`
	EmojiID string `json:"emoji_id,omitempty"`
}

func (bd ButtonData) inline() models.InlineKeyboardButton {
	btn := models.InlineKeyboardButton{Text: bd.Text}
	if bd.Style != "" {
		btn.Style = bd.Style
	}
	if bd.EmojiID != "" {
		btn.IconCustomEmojiID = bd.EmojiID
	}
	return btn
}

func (bd ButtonData) InlineCallback(callbackData string) models.InlineKeyboardButton {
	btn := bd.inline()
	btn.CallbackData = callbackData
	return btn
}

func (bd ButtonData) InlineURL(url string) models.InlineKeyboardButton {
	btn := bd.inline()
	btn.URL = url
	return btn
}

func (bd ButtonData) InlineWebApp(url string) models.InlineKeyboardButton {
	btn := bd.inline()
	btn.WebApp = &models.WebAppInfo{URL: url}
	return btn
}

var validStyles = map[string]bool{
	"danger":  true,
	"success": true,
	"primary": true,
}

type Translation map[string]json.RawMessage

type Manager struct {
	translations    map[string]Translation
	defaultLanguage string
	mu              sync.RWMutex
}

var (
	instance *Manager
	once     sync.Once
)

func GetInstance() *Manager {
	once.Do(func() {
		instance = &Manager{
			translations:    make(map[string]Translation),
			defaultLanguage: "en",
		}
	})
	return instance
}

func (tm *Manager) InitTranslations(translationsDir string, defaultLanguage string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if defaultLanguage != "" {
		tm.defaultLanguage = defaultLanguage
	}

	files, err := os.ReadDir(translationsDir)
	if err != nil {
		return fmt.Errorf("failed to read translation directory: %w", err)
	}

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		langCode := strings.TrimSuffix(file.Name(), ".json")
		filePath := filepath.Join(translationsDir, file.Name())

		content, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read translation file %s: %w", file.Name(), err)
		}

		var translation Translation
		if err := json.Unmarshal(content, &translation); err != nil {
			return fmt.Errorf("failed to parse translation file %s: %w", file.Name(), err)
		}

		for key, raw := range translation {
			if len(raw) > 0 && raw[0] == '{' {
				var bd ButtonData
				if err := json.Unmarshal(raw, &bd); err == nil && bd.Style != "" {
					if !validStyles[bd.Style] {
						return fmt.Errorf("invalid style %q for key %q in %s (must be \"danger\", \"success\" or \"primary\")", bd.Style, key, file.Name())
					}
				}
			}
		}

		tm.translations[langCode] = translation
	}

	if _, exists := tm.translations[tm.defaultLanguage]; !exists {
		return fmt.Errorf("default language %s translation not found", tm.defaultLanguage)
	}

	return nil
}

func (tm *Manager) GetText(langCode, key string) string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if translation, exists := tm.translations[langCode]; exists {
		if raw, exists := translation[key]; exists && len(raw) > 0 {
			if text := extractButton(raw).Text; text != "" {
				return text
			}
		}
	}

	if translation, exists := tm.translations[tm.defaultLanguage]; exists {
		if raw, exists := translation[key]; exists {
			return extractButton(raw).Text
		}
	}

	return key
}

func (tm *Manager) GetButton(langCode, key string) ButtonData {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if translation, exists := tm.translations[langCode]; exists {
		if raw, exists := translation[key]; exists && len(raw) > 0 {
			return extractButton(raw)
		}
	}

	if translation, exists := tm.translations[tm.defaultLanguage]; exists {
		if raw, exists := translation[key]; exists {
			return extractButton(raw)
		}
	}

	return ButtonData{Text: key}
}

func extractButton(raw json.RawMessage) ButtonData {
	if len(raw) == 0 {
		return ButtonData{}
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return ButtonData{Text: s}
		}
	}
	var bd ButtonData
	if err := json.Unmarshal(raw, &bd); err == nil {
		return bd
	}
	return ButtonData{}
}
