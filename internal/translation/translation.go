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
	return models.InlineKeyboardButton{
		Text:              bd.Text,
		Style:             bd.Style,
		IconCustomEmojiID: bd.EmojiID,
	}
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

type translation map[string]ButtonData

type Manager struct {
	translations    map[string]translation
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
			translations:    make(map[string]translation),
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

		var rawMap map[string]json.RawMessage
		if err := json.Unmarshal(content, &rawMap); err != nil {
			return fmt.Errorf("failed to parse translation file %s: %w", file.Name(), err)
		}

		parsed := make(translation, len(rawMap))
		for key, raw := range rawMap {
			bd, err := parseValue(raw)
			if err != nil {
				return fmt.Errorf("failed to parse key %q in %s: %w", key, file.Name(), err)
			}
			if bd.Style != "" && !validStyles[bd.Style] {
				return fmt.Errorf("invalid style %q for key %q in %s (must be \"danger\", \"success\" or \"primary\")", bd.Style, key, file.Name())
			}
			parsed[key] = bd
		}

		tm.translations[langCode] = parsed
	}

	if _, exists := tm.translations[tm.defaultLanguage]; !exists {
		return fmt.Errorf("default language %s translation not found", tm.defaultLanguage)
	}

	return nil
}

func (tm *Manager) GetText(langCode, key string) string {
	return tm.lookup(langCode, key).Text
}

func (tm *Manager) GetButton(langCode, key string) ButtonData {
	return tm.lookup(langCode, key)
}

func (tm *Manager) lookup(langCode, key string) ButtonData {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if t, exists := tm.translations[langCode]; exists {
		if bd, exists := t[key]; exists && bd.Text != "" {
			return bd
		}
	}

	if t, exists := tm.translations[tm.defaultLanguage]; exists {
		if bd, exists := t[key]; exists {
			return bd
		}
	}

	return ButtonData{Text: key}
}

func parseValue(raw json.RawMessage) (ButtonData, error) {
	if len(raw) == 0 {
		return ButtonData{}, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return ButtonData{}, err
		}
		return ButtonData{Text: s}, nil
	}
	var bd ButtonData
	if err := json.Unmarshal(raw, &bd); err != nil {
		return ButtonData{}, err
	}
	return bd, nil
}
