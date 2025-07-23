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
	// Сначала восстанавливаем разбитые плейсхолдеры
	xmlContent = dp.reconstructPlaceholders(xmlContent)
	
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
	placeholderRegex := regexp.MustCompile(`\{([^}]+)\}`)
	placeholders := placeholderRegex.FindAllStringSubmatch(xmlContent, -1)
	
	for _, match := range placeholders {
		placeholder := match[0]
		path := match[1]
		
		value := dp.getValue(data, path)
		xmlContent = strings.Replace(xmlContent, placeholder, dp.formatValue(value), -1)
	}

	return xmlContent, nil
}

// Восстановление разбитых плейсхолдеров
func (dp *DocxProcessor) reconstructPlaceholders(xmlContent string) string {
	// Более простой и надежный подход
	// Сначала ищем все w:t теги и извлекаем из них текст
	wtRegex := regexp.MustCompile(`<w:t[^>]*>([^<]*)</w:t>`)
	
	// Проходим по документу и ищем последовательности w:t тегов, которые могут содержать плейсхолдер
	result := xmlContent
	
	for {
		changed := false
		
		// Ищем паттерны разбитых плейсхолдеров
		patterns := []string{
			// Случай 1: { в одном теге, остальное в других
			`(<w:t[^>]*>[^<]*\{[^<]*</w:t>)([^{]*?)(<w:t[^>]*>[^<]*\}[^<]*</w:t>)`,
			// Случай 2: полностью разбитый плейсхолдер
			`(<w:t[^>]*>)(\{)(<\/w:t>[^{]*<w:t[^>]*>)([^}<]+)(<\/w:t>[^{]*<w:t[^>]*>)(\})(<\/w:t>)`,
			// Случай 3: простой разбитый плейсхолдер между двумя w:t
			`(<w:t[^>]*>[^<]*)\{([^}]*)</w:t>([^{]*)<w:t[^>]*>([^}]*\}[^<]*)(<\/w:t>)`,
		}
		
		for _, pattern := range patterns {
			regex := regexp.MustCompile(pattern)
			if regex.MatchString(result) {
				result = dp.fixBrokenPlaceholderWithPattern(result, regex)
				changed = true
				break
			}
		}
		
		if !changed {
			break
		}
	}
	
	// Финальная обработка - простое объединение текста в w:t тегах для восстановления плейсхолдеров
	result = dp.mergeConsecutiveWtTags(result)
	
	return result
}

// Исправление разбитого плейсхолдера с помощью паттерна
func (dp *DocxProcessor) fixBrokenPlaceholderWithPattern(xmlContent string, regex *regexp.Regexp) string {
	return regex.ReplaceAllStringFunc(xmlContent, func(match string) string {
		// Извлекаем весь текст из w:t тегов в этом фрагменте
		wtRegex := regexp.MustCompile(`<w:t[^>]*>([^<]*)</w:t>`)
		matches := wtRegex.FindAllStringSubmatch(match, -1)
		
		var textBuilder strings.Builder
		for _, m := range matches {
			textBuilder.WriteString(m[1])
		}
		
		fullText := textBuilder.String()
		
		// Проверяем, есть ли в тексте плейсхолдер
		if strings.Contains(fullText, "{") && strings.Contains(fullText, "}") {
			// Возвращаем как один w:t тег
			return fmt.Sprintf("<w:t>%s</w:t>", fullText)
		}
		
		return match
	})
}

// Объединение последовательных w:t тегов для восстановления плейсхолдеров
func (dp *DocxProcessor) mergeConsecutiveWtTags(xmlContent string) string {
	// Ищем группы последовательных w:t тегов, которые могут содержать части плейсхолдера
	runRegex := regexp.MustCompile(`<w:r[^>]*>.*?</w:r>`)
	
	return runRegex.ReplaceAllStringFunc(xmlContent, func(run string) string {
		// В каждом w:r ищем w:t теги
		wtRegex := regexp.MustCompile(`<w:t[^>]*>[^<]*</w:t>`)
		wtMatches := wtRegex.FindAllString(run, -1)
		
		if len(wtMatches) <= 1 {
			return run
		}
		
		// Извлекаем текст из всех w:t тегов
		var combinedText strings.Builder
		var hasPlaceholder bool
		
		for _, wtMatch := range wtMatches {
			textRegex := regexp.MustCompile(`<w:t[^>]*>([^<]*)</w:t>`)
			if textMatch := textRegex.FindStringSubmatch(wtMatch); textMatch != nil {
				text := textMatch[1]
				combinedText.WriteString(text)
				
				if strings.Contains(text, "{") || strings.Contains(text, "}") {
					hasPlaceholder = true
				}
			}
		}
		
		fullText := combinedText.String()
		
		// Если есть признаки плейсхолдера, проверяем полный текст
		if hasPlaceholder {
			placeholderRegex := regexp.MustCompile(`\{[^}]+\}`)
			if placeholderRegex.MatchString(fullText) {
				// Заменяем все w:t теги на один с объединенным текстом
				firstWt := wtMatches[0]
				replacement := regexp.MustCompile(`<w:t[^>]*>[^<]*</w:t>`).ReplaceAllString(firstWt, fmt.Sprintf("<w:t>%s</w:t>", fullText))
				
				// Удаляем остальные w:t теги из run
				result := run
				for i, wtMatch := range wtMatches {
					if i == 0 {
						result = strings.Replace(result, wtMatch, replacement, 1)
					} else {
						result = strings.Replace(result, wtMatch, "", 1)
					}
				}
				return result
			}
		}
		
		return run
	})
}

// Добавляем функцию для тестирования и отладки
func (dp *DocxProcessor) debugPlaceholders(xmlContent string) {
	fmt.Println("=== ОТЛАДКА ПЛЕЙСХОЛДЕРОВ ===")
	
	// Ищем все w:t теги
	wtRegex := regexp.MustCompile(`<w:t[^>]*>([^<]*)</w:t>`)
	matches := wtRegex.FindAllStringSubmatch(xmlContent, -1)
	
	fmt.Printf("Найдено %d w:t тегов:\n", len(matches))
	for i, match := range matches {
		text := match[1]
		if strings.Contains(text, "{") || strings.Contains(text, "}") || strings.Contains(text, "items") || strings.Contains(text, "client") {
			fmt.Printf("  [%d]: '%s'\n", i, text)
		}
	}
	
	// Ищем готовые плейсхолдеры
	placeholderRegex := regexp.MustCompile(`\{([^}]+)\}`)
	placeholders := placeholderRegex.FindAllStringSubmatch(xmlContent, -1)
	
	fmt.Printf("\nГотовые плейсхолдеры (%d):\n", len(placeholders))
	for i, placeholder := range placeholders {
		fmt.Printf("  [%d]: %s\n", i, placeholder[0])
	}
	
	fmt.Println("=== КОНЕЦ ОТЛАДКИ ===\n")
}

// Извлечение таблиц из XML
func (dp *DocxProcessor) extractTables(xmlContent string) []string {
	// Используем более точное регулярное выражение для поиска таблиц
	tableRegex := regexp.MustCompile(`(?s)<w:tbl[^>]*>.*?</w:tbl>`)
	return tableRegex.FindAllString(xmlContent, -1)
}

// Обработка таблицы
func (dp *DocxProcessor) processTable(tableXML string, data interface{}) (string, error) {
	fmt.Printf("=== ОБРАБОТКА ТАБЛИЦЫ ===\n")
	fmt.Printf("Размер таблицы: %d символов\n", len(tableXML))
	
	// Сначала восстанавливаем разбитые плейсхолдеры в таблице
	originalTable := tableXML
	tableXML = dp.reconstructPlaceholders(tableXML)
	
	if originalTable != tableXML {
		fmt.Println("Плейсхолдеры были восстановлены в таблице")
	}
	
	// Находим все строки в таблице
	rowRegex := regexp.MustCompile(`(?s)<w:tr[^>]*>.*?</w:tr>`)
	rows := rowRegex.FindAllString(tableXML, -1)
	
	fmt.Printf("Найдено строк в таблице: %d\n", len(rows))
	
	if len(rows) == 0 {
		return tableXML, nil
	}

	// Ищем плейсхолдеры во всех строках
	var templateRows []TableRowInfo
	placeholderRegex := regexp.MustCompile(`\{([^}]+)\}`)
	
	for i, row := range rows {
		placeholders := placeholderRegex.FindAllStringSubmatch(row, -1)
		if len(placeholders) > 0 {
			fmt.Printf("Строка %d содержит %d плейсхолдеров: ", i, len(placeholders))
			for _, p := range placeholders {
				fmt.Printf("%s ", p[0])
			}
			fmt.Println()
			
			templateRows = append(templateRows, TableRowInfo{
				Index:        i,
				Content:      row,
				Placeholders: placeholders,
			})
		}
	}

	if len(templateRows) == 0 {
		fmt.Println("Плейсхолдеры в таблице не найдены")
		return tableXML, nil
	}

	// Обрабатываем каждую строку с плейсхолдерами
	result := tableXML
	
	// Обрабатываем строки в обратном порядке, чтобы не нарушить индексы
	for i := len(templateRows) - 1; i >= 0; i-- {
		templateRow := templateRows[i]
		
		// Определяем массивы для размножения строк
		arrays := dp.findArraysInRow(templateRow.Content, data)
		fmt.Printf("Найдено массивов в строке %d: %v\n", templateRow.Index, arrays)
		
		if len(arrays) == 0 {
			// Нет массивов, просто заменяем плейсхолдеры
			processedRow := dp.replaceRowPlaceholders(templateRow.Content, data, 0)
			result = strings.Replace(result, templateRow.Content, processedRow, 1)
		} else {
			// Генерируем новые строки на основе массивов
			newRows := dp.generateTableRows(templateRow.Content, data, arrays)
			fmt.Printf("Сгенерировано строк: %d\n", len(newRows))
			
			// Заменяем шаблонную строку на сгенерированные
			allNewRows := strings.Join(newRows, "")
			result = strings.Replace(result, templateRow.Content, allNewRows, 1)
		}
	}
	
	fmt.Println("=== КОНЕЦ ОБРАБОТКИ ТАБЛИЦЫ ===\n")
	return result, nil
}

// Структура для информации о строке таблицы
type TableRowInfo struct {
	Index        int
	Content      string
	Placeholders [][]string
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

	fmt.Printf("Генерируем строки для массивов: %v\n", arrays)

	// Определяем стратегию генерации строк на основе структуры массивов
	maxRows := dp.calculateMaxRows(data, arrays)
	fmt.Printf("Максимальное количество строк: %d\n", maxRows)

	rows := make([]string, 0, maxRows)
	
	// Генерируем строки с учетом вложенности массивов
	for i := 0; i < maxRows; i++ {
		newRow := dp.replaceRowPlaceholdersAdvanced(templateRow, data, arrays, i)
		rows = append(rows, newRow)
	}

	return rows
}

// Продвинутая замена плейсхолдеров с учетом множественных массивов
func (dp *DocxProcessor) replaceRowPlaceholdersAdvanced(rowXML string, data interface{}, arrays []string, globalIndex int) string {
	placeholderRegex := regexp.MustCompile(`\{([^}]+)\}`)
	
	return placeholderRegex.ReplaceAllStringFunc(rowXML, func(match string) string {
		path := strings.Trim(match, "{}")
		
		// Определяем индексы для каждого уровня массива
		adjustedPath := dp.calculatePathIndices(path, data, arrays, globalIndex)
		value := dp.getValue(data, adjustedPath)
		
		return dp.formatValue(value)
	})
}

// Вычисление максимального количества строк с учетом вложенных массивов  
func (dp *DocxProcessor) calculateMaxRows(data interface{}, arrays []string) int {
	maxRows := 1
	
	// Группируем массивы по уровням вложенности
	arrayLevels := make(map[string][]string)
	
	for _, arrayPath := range arrays {
		level := dp.getArrayLevel(arrayPath)
		arrayLevels[level] = append(arrayLevels[level], arrayPath)
	}
	
	// Вычисляем общее количество строк
	totalRows := 0
	
	// Если есть массив верхнего уровня (например, items)
	topLevelArrays := dp.findTopLevelArrays(arrays)
	
	if len(topLevelArrays) > 0 {
		for _, topArray := range topLevelArrays {
			topArrayValue := dp.getValue(data, topArray)
			topArrayLength := dp.getArrayLength(topArrayValue)
			
			// Для каждого элемента верхнего массива считаем вложенные
			for i := 0; i < topArrayLength; i++ {
				nestedRows := dp.calculateNestedRows(data, arrays, topArray, i)
				totalRows += nestedRows
			}
		}
		return totalRows
	}
	
	// Если нет массивов верхнего уровня, берем максимальную длину
	for _, arrayPath := range arrays {
		arrayValue := dp.getValue(data, arrayPath)
		length := dp.getArrayLength(arrayValue)
		if length > maxRows {
			maxRows = length
		}
	}
	
	return maxRows
}

// Поиск массивов верхнего уровня
func (dp *DocxProcessor) findTopLevelArrays(arrays []string) []string {
	var topLevel []string
	
	for _, arrayPath := range arrays {
		parts := strings.Split(arrayPath, ".")
		if len(parts) == 1 {
			topLevel = append(topLevel, arrayPath)
		}
	}
	
	return topLevel
}

// Получение уровня массива
func (dp *DocxProcessor) getArrayLevel(arrayPath string) string {
	parts := strings.Split(arrayPath, ".")
	if len(parts) == 1 {
		return "level1"
	} else if len(parts) == 2 {
		return "level2"
	}
	return "level3+"
}

// Вычисление количества вложенных строк
func (dp *DocxProcessor) calculateNestedRows(data interface{}, arrays []string, topArray string, topIndex int) int {
	rows := 1
	
	// Ищем вложенные массивы для данного элемента верхнего массива
	for _, arrayPath := range arrays {
		if strings.HasPrefix(arrayPath, topArray+".") {
			// Это вложенный массив
			indexedPath := fmt.Sprintf("%s[%d].%s", topArray, topIndex, 
				strings.TrimPrefix(arrayPath, topArray+"."))
			
			nestedValue := dp.getValue(data, indexedPath)
			nestedLength := dp.getArrayLength(nestedValue)
			
			if nestedLength > rows {
				rows = nestedLength
			}
		}
	}
	
	return rows
}

// Вычисление индексов для пути с учетом множественных массивов
func (dp *DocxProcessor) calculatePathIndices(path string, data interface{}, arrays []string, globalIndex int) string {
	parts := strings.Split(path, ".")
	result := make([]string, len(parts))
	copy(result, parts)
	
	currentIndex := globalIndex
	
	// Проходим по частям пути и добавляем индексы где нужно
	for i, part := range parts {
		currentPath := strings.Join(parts[:i+1], ".")
		
		// Проверяем, является ли эта часть массивом
		if dp.containsArray(arrays, currentPath) {
			// Вычисляем правильный индекс для этого уровня
			index := dp.calculateIndexForLevel(data, arrays, currentPath, currentIndex)
			result[i] = fmt.Sprintf("%s[%d]", part, index)
			
			// Обновляем текущий индекс для следующих уровней
			currentIndex = dp.adjustIndexForNextLevel(data, currentPath, index, currentIndex)
		}
	}
	
	return strings.Join(result, ".")
}

// Проверка, содержится ли путь в массивах
func (dp *DocxProcessor) containsArray(arrays []string, path string) bool {
	for _, arrayPath := range arrays {
		if arrayPath == path {
			return true
		}
	}
	return false
}

// Вычисление индекса для уровня
func (dp *DocxProcessor) calculateIndexForLevel(data interface{}, arrays []string, currentPath string, globalIndex int) int {
	// Простая стратегия: для массивов верхнего уровня используем прямое деление
	parts := strings.Split(currentPath, ".")
	
	if len(parts) == 1 {
		// Массив верхнего уровня
		return globalIndex / dp.getNestedMultiplier(data, arrays, currentPath)
	} else {
		// Вложенный массив
		parentPath := strings.Join(parts[:len(parts)-1], ".")
		return globalIndex % dp.getArrayLengthByPath(data, currentPath)
	}
}

// Получение множителя для вложенных массивов
func (dp *DocxProcessor) getNestedMultiplier(data interface{}, arrays []string, topArrayPath string) int {
	multiplier := 1
	
	for _, arrayPath := range arrays {
		if strings.HasPrefix(arrayPath, topArrayPath+".") && arrayPath != topArrayPath {
			// Находим максимальную длину вложенного массива
			maxLength := 0
			topArray := dp.getValue(data, topArrayPath)
			if topArray != nil {
				topLength := dp.getArrayLength(topArray)
				for i := 0; i < topLength; i++ {
					nestedPath := fmt.Sprintf("%s[%d].%s", topArrayPath, i, 
						strings.TrimPrefix(arrayPath, topArrayPath+"."))
					nestedArray := dp.getValue(data, nestedPath)
					length := dp.getArrayLength(nestedArray)
					if length > maxLength {
						maxLength = length
					}
				}
			}
			if maxLength > multiplier {
				multiplier = maxLength
			}
		}
	}
	
	return multiplier
}

// Получение длины массива по пути
func (dp *DocxProcessor) getArrayLengthByPath(data interface{}, path string) int {
	value := dp.getValue(data, path)
	return dp.getArrayLength(value)
}

// Корректировка индекса для следующего уровня
func (dp *DocxProcessor) adjustIndexForNextLevel(data interface{}, currentPath string, currentIndex int, globalIndex int) int {
	// Возвращаем остаток для следующего уровня
	arrayLength := dp.getArrayLengthByPath(data, currentPath)
	if arrayLength > 0 {
		return globalIndex % arrayLength
	}
	return 0
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
			arrayName := part[:strings.Index(p