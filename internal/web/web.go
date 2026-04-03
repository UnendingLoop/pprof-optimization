package web

import (
	"html/template"
	"net/http"
	"path/filepath"
	"sync"
)

var (
	tplCache *template.Template
	once     sync.Once
)

// LoadTemplates инициализирует шаблоны один раз при старте
func LoadTemplates() {
	once.Do(func() {
		tplCache = template.Must(template.ParseGlob(filepath.Join("internal", "web", "*.gohtml")))
	})
}

// Render рендерит HTML-страницу
func Render(w http.ResponseWriter, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err := tplCache.ExecuteTemplate(w, name+".gohtml", data)
	if err != nil {
		http.Error(w, "Ошибка рендера шаблона: "+err.Error(), http.StatusInternalServerError)
	}
}
