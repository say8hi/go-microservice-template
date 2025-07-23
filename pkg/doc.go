package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// Структура для работы с DOCX
type DocxProcessor struct {
	zipReader *zip.ReadCloser
	files     map[string][]byte
}

// Структура для XML элементов Word документа
type WordDocument struct {
	XMLName xml.Name `xml:"document"`
	Body    Body     `xml:"body"`
}

type Body struct {
	Tables []Table `xml:"tbl"`
	Paras  []Para  `xml:"p"`
}

type Table struct {
	Rows []Row `xml:"tr"`
}

type Row struct {
	Cells []Cell `xml:"tc"`
}

type Cell struct {
	Paras []Para `xml:"p"`
}

type Para struct {
	Runs []Run `xml:"r"`
	Text string `xml:",innerxml"`
}

type Run struct {
	Text string `xml:"t"`
}

// Создание нового процессора
func NewDocxProcessor(filepath string) (*DocxProcessor, error) {
	zipReader, err := zip.OpenReader(filepath)
	if err != nil {
		return nil, fmt.Errorf("ошибка открытия DOCX файла: %v", err)
	}

	processor := &DocxProcessor{
		zipReader: zipReader,
		files:     make(map[string][]byte),
	}

	// Читаем все файлы из архива
	for _, file := range zipReader.File {
		content, err := readZipFile(file)
		if err != nil {
			return nil, fmt.Errorf("ошибка чтения файла %s: %v", file.Name, err)
		}
		processor.files[file.Name] = content
	}

	return processor, nil
}

// Чтение файла из ZIP архива
func readZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

// Основная функция обработки
func (dp *DocxProcessor) Process(jsonData string, outputPath string) error {
	// Парсим JSON данные
	var data interface{}
	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return fmt.Errorf("ошибка парсинга JSON: %v", err)
	}

	// Обрабатываем документ
	documentXML, exists := dp.files["word/document.xml"]
	if !exists {
		return fmt.Errorf("файл word/document.xml не найден")
	}

	// Заменяем плейсхолдеры
	processedXML, err := dp.processDocument(string(documentXML), data)
	if err != nil {
		return fmt.Errorf("ошибка обработки документа: %v", err)
	}

	dp.files["word/document.xml"] = []byte(processedXML)

	// Сохраняем результат
	return dp.saveDocument(outputPath)
}

// Обработка документа с заменой плейсхолдеров
func (dp *DocxProcessor) processDocument(xmlContent string, data interface{}) (string, error) {
	// Находим все плейсхолдеры в формате {path}
	placeholderRegex := regexp.MustCompile(`\{([^}]+)\}`)
	placeholders := placeholderRegex.FindAllStringSubmatch(xmlContent, -1)

	// Группируем плейсхолдеры по таблицам
	tables := dp.extractTables(xmlContent)
	
	for i, table := range tables {
		processedTable, err := dp.processTable(table, data)
		if err != nil {
			return "", fmt.Errorf("ошибка обработки таблицы %d: %v", i, err)
		}
		xmlContent = strings.Replace(xmlContent, table, processedTable, 1)
	}

	// Обрабатываем оставшиеся плейсхолдеры вне таблиц
	for _, match := range placeholders {
		if !strings.Contains(xmlContent, match[0]) {
			continue // Уже обработан в таблице
		}
		
		placeholder := match[0]
		path := match[1]
		
		value := dp.getValue(data, path)
		xmlContent = strings.Replace(xmlContent, placeholder, dp.formatValue(value), -1)
	}

	return xmlContent, nil
}

// Извлечение таблиц из XML
func (dp *DocxProcessor) extractTables(xmlContent string) []string {
	tableRegex := regexp.MustCompile(`<w:tbl[^>]*>.*?</w:tbl>`)
	return tableRegex.FindAllString(xmlContent, -1)
}

// Обработка таблицы
func (dp *DocxProcessor) processTable(tableXML string, data interface{}) (string, error) {
	// Находим все строки в таблице
	rowRegex := regexp.MustCompile(`<w:tr[^>]*>.*?</w:tr>`)
	rows := rowRegex.FindAllString(tableXML, -1)
	
	if len(rows) == 0 {
		return tableXML, nil
	}

	// Ищем плейсхолдеры в первой строке данных (обычно вторая строка после заголовка)
	var templateRow string
	var templateRowIndex int = -1
	
	for i, row := range rows {
		if strings.Contains(row, "{") {
			templateRow = row
			templateRowIndex = i
			break
		}
	}

	if templateRowIndex == -1 {
		// Нет плейсхолдеров в таблице, обрабатываем как обычный текст
		placeholderRegex := regexp.MustCompile(`\{([^}]+)\}`)
		placeholders := placeholderRegex.FindAllStringSubmatch(tableXML, -1)
		
		result := tableXML
		for _, match := range placeholders {
			placeholder := match[0]
			path := match[1]
			value := dp.getValue(data, path)
			result = strings.Replace(result, placeholder, dp.formatValue(value), -1)
		}
		return result, nil
	}

	// Определяем массивы для размножения строк
	arrays := dp.findArraysInRow(templateRow, data)
	
	if len(arrays) == 0 {
		// Нет массивов, просто заменяем плейсхолдеры
		processedRow := dp.replaceRowPlaceholders(templateRow, data, 0)
		result := tableXML
		result = strings.Replace(result, templateRow, processedRow, 1)
		return result, nil
	}

	// Генерируем новые строки на основе массивов
	newRows := dp.generateTableRows(templateRow, data, arrays)
	
	// Заменяем шаблонную строку на сгенерированные
	result := tableXML
	allNewRows := strings.Join(newRows, "")
	result = strings.Replace(result, templateRow, allNewRows, 1)
	
	return result, nil
}

// Поиск массивов в строке таблицы
func (dp *DocxProcessor) findArraysInRow(rowXML string, data interface{}) []string {
	placeholderRegex := regexp.MustCompile(`\{([^}]+)\}`)
	placeholders := placeholderRegex.FindAllStringSubmatch(rowXML, -1)
	
	arrays := make([]string, 0)
	seen := make(map[string]bool)
	
	for _, match := range placeholders {
		path := match[1]
		basePath := dp.getBasePath(path)
		
		if seen[basePath] {
			continue
		}
		
		value := dp.getValue(data, basePath)
		if dp.isArray(value) {
			arrays = append(arrays, basePath)
			seen[basePath] = true
		}
	}
	
	return arrays
}

// Получение базового пути (до индекса массива)
func (dp *DocxProcessor) getBasePath(path string) string {
	parts := strings.Split(path, ".")
	for i, part := range parts {
		if strings.Contains(part, "[") {
			return strings.Join(parts[:i+1], ".")
		}
	}
	return path
}

// Генерация строк таблицы на основе массивов
func (dp *DocxProcessor) generateTableRows(templateRow string, data interface{}, arrays []string) []string {
	if len(arrays) == 0 {
		return []string{dp.replaceRowPlaceholders(templateRow, data, 0)}
	}

	// Находим максимальную длину массива
	maxLength := 0
	for _, arrayPath := range arrays {
		arrayValue := dp.getValue(data, arrayPath)
		length := dp.getArrayLength(arrayValue)
		if length > maxLength {
			maxLength = length
		}
	}

	// Генерируем строки
	rows := make([]string, 0, maxLength)
	for i := 0; i < maxLength; i++ {
		newRow := dp.replaceRowPlaceholders(templateRow, data, i)
		rows = append(rows, newRow)
	}

	return rows
}

// Замена плейсхолдеров в строке
func (dp *DocxProcessor) replaceRowPlaceholders(rowXML string, data interface{}, arrayIndex int) string {
	placeholderRegex := regexp.MustCompile(`\{([^}]+)\}`)
	
	return placeholderRegex.ReplaceAllStringFunc(rowXML, func(match string) string {
		path := strings.Trim(match, "{}")
		
		// Добавляем индекс массива если нужно
		adjustedPath := dp.adjustPathForArray(path, data, arrayIndex)
		value := dp.getValue(data, adjustedPath)
		
		return dp.formatValue(value)
	})
}

// Корректировка пути для массива
func (dp *DocxProcessor) adjustPathForArray(path string, data interface{}, arrayIndex int) string {
	parts := strings.Split(path, ".")
	
	// Ищем массивы в пути и добавляем индексы
	for i, part := range parts {
		currentPath := strings.Join(parts[:i+1], ".")
		value := dp.getValue(data, currentPath)
		
		if dp.isArray(value) {
			// Добавляем индекс массива
			parts[i] = fmt.Sprintf("%s[%d]", part, arrayIndex)
		}
	}
	
	return strings.Join(parts, ".")
}

// Получение значения по пути
func (dp *DocxProcessor) getValue(data interface{}, path string) interface{} {
	if path == "" {
		return nil
	}

	parts := strings.Split(path, ".")
	current := data

	for _, part := range parts {
		// Обработка массивов
		if strings.Contains(part, "[") {
			arrayName := part[:strings.Index(part, "[")]
			indexStr := part[strings.Index(part, "[")+1 : strings.Index(part, "]")]
			index, err := strconv.Atoi(indexStr)
			if err != nil {
				return nil
			}

			current = dp.getFieldValue(current, arrayName)
			if current == nil {
				return nil
			}

			arr := reflect.ValueOf(current)
			if arr.Kind() != reflect.Slice || index >= arr.Len() {
				return nil
			}
			current = arr.Index(index).Interface()
		} else {
			current = dp.getFieldValue(current, part)
			if current == nil {
				return nil
			}
		}
	}

	return current
}

// Получение значения поля
func (dp *DocxProcessor) getFieldValue(data interface{}, field string) interface{} {
	if data == nil {
		return nil
	}

	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Map:
		mapValue := v.Interface().(map[string]interface{})
		return mapValue[field]
	case reflect.Struct:
		fieldValue := v.FieldByName(field)
		if !fieldValue.IsValid() {
			return nil
		}
		return fieldValue.Interface()
	}

	return nil
}

// Проверка, является ли значение массивом
func (dp *DocxProcessor) isArray(value interface{}) bool {
	if value == nil {
		return false
	}
	
	v := reflect.ValueOf(value)
	return v.Kind() == reflect.Slice
}

// Получение длины массива
func (dp *DocxProcessor) getArrayLength(value interface{}) int {
	if value == nil {
		return 0
	}
	
	v := reflect.ValueOf(value)
	if v.Kind() != reflect.Slice {
		return 0
	}
	
	return v.Len()
}

// Форматирование значения
func (dp *DocxProcessor) formatValue(value interface{}) string {
	if value == nil {
		return ""
	}
	
	return fmt.Sprintf("%v", value)
}

// Сохранение документа
func (dp *DocxProcessor) saveDocument(outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("ошибка создания выходного файла: %v", err)
	}
	defer file.Close()

	zipWriter := zip.NewWriter(file)
	defer zipWriter.Close()

	// Записываем все файлы в новый архив
	for filename, content := range dp.files {
		writer, err := zipWriter.Create(filename)
		if err != nil {
			return fmt.Errorf("ошибка создания файла %s в архиве: %v", filename, err)
		}

		_, err = writer.Write(content)
		if err != nil {
			return fmt.Errorf("ошибка записи файла %s: %v", filename, err)
		}
	}

	return nil
}

// Закрытие процессора
func (dp *DocxProcessor) Close() error {
	if dp.zipReader != nil {
		return dp.zipReader.Close()
	}
	return nil
}

// Пример использования
func main() {
	// Пример JSON данных
	jsonData := `{
		"client": {
			"Name": "ООО Компания",
			"Address": "г. Москва, ул. Примерная, д. 1"
		},
		"items": [
			{
				"Name": "Товар 1",
				"Types": [
					{
						"Name": "Тип A",
						"Price": 1000
					},
					{
						"Name": "Тип B", 
						"Price": 1500
					}
				]
			},
			{
				"Name": "Товар 2",
				"Types": [
					{
						"Name": "Тип C",
						"Price": 2000
					}
				]
			}
		]
	}`

	// Создаем процессор
	processor, err := NewDocxProcessor("template.docx")
	if err != nil {
		log.Fatalf("Ошибка создания процессора: %v", err)
	}
	defer processor.Close()

	// Обрабатываем документ
	err = processor.Process(jsonData, "output.docx")
	if err != nil {
		log.Fatalf("Ошибка обработки документа: %v", err)
	}

	fmt.Println("Документ успешно обработан и сохранен как output.docx")
}