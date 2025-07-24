package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// DocumentProcessor обрабатывает DOCX документы
type DocumentProcessor struct {
	zipReader *zip.ReadCloser
	files     map[string][]byte
}

// TableRow представляет строку таблицы
type TableRow struct {
	Cells []string
}

// Table представляет таблицу в документе
type Table struct {
	Rows []TableRow
}

// NewDocumentProcessor создает новый процессор документов
func NewDocumentProcessor(filename string) (*DocumentProcessor, error) {
	r, err := zip.OpenReader(filename)
	if err != nil {
		return nil, err
	}

	dp := &DocumentProcessor{
		zipReader: r,
		files:     make(map[string][]byte),
	}

	// Читаем все файлы из архива
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		
		content, err := io.ReadAll(rc)
		if err != nil {
			rc.Close()
			return nil, err
		}
		rc.Close()
		
		dp.files[f.Name] = content
	}

	return dp, nil
}

// Close закрывает процессор
func (dp *DocumentProcessor) Close() error {
	return dp.zipReader.Close()
}

// ProcessWithJSON обрабатывает документ с JSON данными
func (dp *DocumentProcessor) ProcessWithJSON(jsonData string) error {
	var data interface{}
	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return fmt.Errorf("ошибка парсинга JSON: %v", err)
	}

	// Обрабатываем основной документ
	if content, exists := dp.files["word/document.xml"]; exists {
		processed, err := dp.processContent(string(content), data)
		if err != nil {
			return err
		}
		dp.files["word/document.xml"] = []byte(processed)
	}

	return nil
}

// processContent обрабатывает содержимое документа
func (dp *DocumentProcessor) processContent(content string, data interface{}) (string, error) {
	// Сначала обрабатываем таблицы
	content = dp.processTables(content, data)
	
	// Затем обрабатываем обычные плейсхолдеры
	content = dp.processPlaceholders(content, data)
	
	return content, nil
}

// processTables обрабатывает таблицы в документе
func (dp *DocumentProcessor) processTables(content string, data interface{}) string {
	// Находим все таблицы
	tableRegex := regexp.MustCompile(`<w:tbl[^>]*>.*?</w:tbl>`)
	tables := tableRegex.FindAllString(content, -1)
	
	for _, table := range tables {
		processedTable := dp.processTable(table, data)
		content = strings.Replace(content, table, processedTable, 1)
	}
	
	return content
}

// processTable обрабатывает отдельную таблицу
func (dp *DocumentProcessor) processTable(tableXML string, data interface{}) string {
	// Находим все строки таблицы
	rowRegex := regexp.MustCompile(`<w:tr[^>]*>.*?</w:tr>`)
	rows := rowRegex.FindAllString(tableXML, -1)
	
	if len(rows) == 0 {
		return tableXML
	}
	
	var processedRows []string
	headerProcessed := false
	
	for _, row := range rows {
		// Находим плейсхолдеры в строке
		placeholders := dp.findPlaceholders(row)
		
		if len(placeholders) == 0 || headerProcessed {
			// Обычная строка без плейсхолдеров или заголовок уже обработан
			processedRows = append(processedRows, dp.processPlaceholders(row, data))
			headerProcessed = true
			continue
		}
		
		// Это строка с плейсхолдерами - нужно создать несколько строк
		arrayPlaceholders := dp.findArrayPlaceholders(placeholders, data)
		
		if len(arrayPlaceholders) == 0 {
			// Нет массивов, обрабатываем как обычную строку
			processedRows = append(processedRows, dp.processPlaceholders(row, data))
		} else {
			// Есть массивы, создаем строки для каждого элемента
			maxLength := dp.getMaxArrayLength(arrayPlaceholders, data)
			
			for i := 0; i < maxLength; i++ {
				newRow := dp.processRowWithIndex(row, data, i)
				processedRows = append(processedRows, newRow)
			}
		}
		headerProcessed = true
	}
	
	// Собираем таблицу обратно
	processedTable := tableXML
	for i, originalRow := range rows {
		if i < len(processedRows) {
			processedTable = strings.Replace(processedTable, originalRow, processedRows[i], 1)
		}
	}
	
	// Если нужно добавить дополнительные строки
	if len(processedRows) > len(rows) {
		// Находим последнюю строку и добавляем после неё
		lastRowIndex := strings.LastIndex(processedTable, "</w:tr>")
		if lastRowIndex != -1 {
			before := processedTable[:lastRowIndex+7]
			after := processedTable[lastRowIndex+7:]
			
			additional := ""
			for i := len(rows); i < len(processedRows); i++ {
				additional += processedRows[i]
			}
			
			processedTable = before + additional + after
		}
	}
	
	return processedTable
}

// findPlaceholders находит все плейсхолдеры в тексте
func (dp *DocumentProcessor) findPlaceholders(text string) []string {
	placeholderRegex := regexp.MustCompile(`\{([^}]+)\}`)
	matches := placeholderRegex.FindAllStringSubmatch(text, -1)
	
	var placeholders []string
	for _, match := range matches {
		if len(match) > 1 {
			placeholders = append(placeholders, match[1])
		}
	}
	
	return placeholders
}

// findArrayPlaceholders находит плейсхолдеры, которые ссылаются на массивы
func (dp *DocumentProcessor) findArrayPlaceholders(placeholders []string, data interface{}) []string {
	var arrayPlaceholders []string
	
	for _, placeholder := range placeholders {
		if dp.isArrayPlaceholder(placeholder, data) {
			arrayPlaceholders = append(arrayPlaceholders, placeholder)
		}
	}
	
	return arrayPlaceholders
}

// isArrayPlaceholder проверяет, является ли плейсхолдер ссылкой на массив
func (dp *DocumentProcessor) isArrayPlaceholder(placeholder string, data interface{}) bool {
	value := dp.getNestedValue(data, placeholder)
	if value == nil {
		return false
	}
	
	switch v := value.(type) {
	case []interface{}:
		return len(v) > 0
	default:
		return false
	}
}

// getMaxArrayLength возвращает максимальную длину среди всех массивов в плейсхолдерах
func (dp *DocumentProcessor) getMaxArrayLength(placeholders []string, data interface{}) int {
	maxLength := 0
	
	for _, placeholder := range placeholders {
		value := dp.getNestedValue(data, placeholder)
		if arr, ok := value.([]interface{}); ok {
			if len(arr) > maxLength {
				maxLength = len(arr)
			}
		}
	}
	
	return maxLength
}

// processRowWithIndex обрабатывает строку таблицы с конкретным индексом массива
func (dp *DocumentProcessor) processRowWithIndex(row string, data interface{}, index int) string {
	placeholderRegex := regexp.MustCompile(`\{([^}]+)\}`)
	
	return placeholderRegex.ReplaceAllStringFunc(row, func(match string) string {
		placeholder := strings.Trim(match, "{}")
		
		// Получаем значение
		value := dp.getNestedValue(data, placeholder)
		
		if arr, ok := value.([]interface{}); ok {
			if index < len(arr) {
				return dp.valueToString(arr[index])
			}
			return ""
		}
		
		return dp.valueToString(value)
	})
}

// processPlaceholders обрабатывает обычные плейсхолдеры
func (dp *DocumentProcessor) processPlaceholders(content string, data interface{}) string {
	placeholderRegex := regexp.MustCompile(`\{([^}]+)\}`)
	
	return placeholderRegex.ReplaceAllStringFunc(content, func(match string) string {
		placeholder := strings.Trim(match, "{}")
		value := dp.getNestedValue(data, placeholder)
		return dp.valueToString(value)
	})
}

// getNestedValue получает значение по вложенному пути (например, "client.name")
func (dp *DocumentProcessor) getNestedValue(data interface{}, path string) interface{} {
	parts := strings.Split(path, ".")
	current := data
	
	for _, part := range parts {
		switch v := current.(type) {
		case map[string]interface{}:
			current = v[part]
		case []interface{}:
			// Если это массив и часть - число, берем элемент по индексу
			if index, err := strconv.Atoi(part); err == nil && index < len(v) {
				current = v[index]
			} else {
				return nil
			}
		default:
			return nil
		}
		
		if current == nil {
			return nil
		}
	}
	
	return current
}

// valueToString преобразует значение в строку
func (dp *DocumentProcessor) valueToString(value interface{}) string {
	if value == nil {
		return ""
	}
	
	switch v := value.(type) {
	case string:
		return v
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", v)
	}
}

// SaveAs сохраняет обработанный документ
func (dp *DocumentProcessor) SaveAs(filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	
	zipWriter := zip.NewWriter(file)
	defer zipWriter.Close()
	
	// Записываем все файлы в новый архив
	for name, content := range dp.files {
		writer, err := zipWriter.Create(name)
		if err != nil {
			return err
		}
		
		_, err = writer.Write(content)
		if err != nil {
			return err
		}
	}
	
	return nil
}

// Пример использования
func main() {
	if len(os.Args) < 4 {
		fmt.Println("Использование: program <input.docx> <data.json> <output.docx>")
		os.Exit(1)
	}
	
	inputFile := os.Args[1]
	dataFile := os.Args[2]
	outputFile := os.Args[3]
	
	// Читаем JSON данные
	jsonData, err := os.ReadFile(dataFile)
	if err != nil {
		fmt.Printf("Ошибка чтения JSON файла: %v\n", err)
		os.Exit(1)
	}
	
	// Создаем процессор документов
	processor, err := NewDocumentProcessor(inputFile)
	if err != nil {
		fmt.Printf("Ошибка открытия DOCX файла: %v\n", err)
		os.Exit(1)
	}
	defer processor.Close()
	
	// Обрабатываем документ
	err = processor.ProcessWithJSON(string(jsonData))
	if err != nil {
		fmt.Printf("Ошибка обработки документа: %v\n", err)
		os.Exit(1)
	}
	
	// Сохраняем результат
	err = processor.SaveAs(outputFile)
	if err != nil {
		fmt.Printf("Ошибка сохранения файла: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Println("Документ успешно обработан!")
}

// Пример JSON структуры:
/*
{
  "client": {
    "name": "ООО Пример",
    "address": "г. Москва, ул. Примерная, д. 1"
  },
  "items": [
    {
      "name": "Товар 1",
      "types": [
        {"name": "Тип A", "price": 100},
        {"name": "Тип B", "price": 150}
      ]
    },
    {
      "name": "Товар 2", 
      "types": [
        {"name": "Тип C", "price": 200}
      ]
    }
  ]
}
*/