package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// DocxProcessor обрабатывает DOCX файлы с заменой плейсхолдеров
type DocxProcessor struct {
	data map[string]interface{}
}

// NewDocxProcessor создает новый процессор
func NewDocxProcessor(jsonData []byte) (*DocxProcessor, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return nil, fmt.Errorf("ошибка парсинга JSON: %v", err)
	}
	
	return &DocxProcessor{data: data}, nil
}

// ProcessFile обрабатывает DOCX файл
func (p *DocxProcessor) ProcessFile(inputPath, outputPath string) error {
	// Открываем исходный файл
	zipReader, err := zip.OpenReader(inputPath)
	if err != nil {
		return fmt.Errorf("ошибка открытия DOCX файла: %v", err)
	}
	defer zipReader.Close()

	// Создаем новый ZIP файл
	outputFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("ошибка создания выходного файла: %v", err)
	}
	defer outputFile.Close()

	zipWriter := zip.NewWriter(outputFile)
	defer zipWriter.Close()

	// Обрабатываем каждый файл в архиве
	for _, file := range zipReader.File {
		if err := p.processZipFile(file, zipWriter); err != nil {
			return fmt.Errorf("ошибка обработки файла %s: %v", file.Name, err)
		}
	}

	return nil
}

// processZipFile обрабатывает отдельный файл в ZIP архиве
func (p *DocxProcessor) processZipFile(file *zip.File, zipWriter *zip.Writer) error {
	reader, err := file.Open()
	if err != nil {
		return err
	}
	defer reader.Close()

	content, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	// Обрабатываем только document.xml (основной контент)
	if file.Name == "word/document.xml" {
		content = p.processXMLContent(content)
	}

	// Создаем файл в новом архиве
	writer, err := zipWriter.Create(file.Name)
	if err != nil {
		return err
	}

	_, err = writer.Write(content)
	return err
}

// processXMLContent обрабатывает XML контент документа
func (p *DocxProcessor) processXMLContent(content []byte) []byte {
	xmlStr := string(content)
	
	// Находим все плейсхолдеры вида {key} или {array.field}
	placeholderRegex := regexp.MustCompile(`\{([^}]+)\}`)
	
	// Сначала обрабатываем таблицы с массивами
	xmlStr = p.processTableArrays(xmlStr)
	
	// Затем заменяем обычные плейсхолдеры
	xmlStr = placeholderRegex.ReplaceAllStringFunc(xmlStr, func(match string) string {
		key := strings.Trim(match, "{}")
		if value := p.getValue(key); value != "" {
			return value
		}
		return match // Оставляем как есть, если значение не найдено
	})

	return []byte(xmlStr)
}

// processTableArrays обрабатывает таблицы с массивами данных
func (p *DocxProcessor) processTableArrays(xmlStr string) string {
	// Находим таблицы
	tableRegex := regexp.MustCompile(`<w:tbl[^>]*>.*?</w:tbl>`)
	
	return tableRegex.ReplaceAllStringFunc(xmlStr, func(table string) string {
		return p.processTable(table)
	})
}

// processTable обрабатывает отдельную таблицу с поддержкой многоуровневых массивов
func (p *DocxProcessor) processTable(table string) string {
	// Ищем строки таблицы
	rowRegex := regexp.MustCompile(`<w:tr[^>]*>.*?</w:tr>`)
	rows := rowRegex.FindAllString(table, -1)
	
	if len(rows) == 0 {
		return table
	}

	// Ищем плейсхолдеры массивов в первой строке данных (обычно вторая строка после заголовка)
	dataRowIndex := 1
	if len(rows) <= dataRowIndex {
		dataRowIndex = 0
	}
	
	dataRow := rows[dataRowIndex]
	
	// Генерируем все возможные комбинации строк с учетом вложенных массивов
	expandedRows := p.expandRowsWithNestedArrays(dataRow)
	
	if len(expandedRows) <= 1 {
		return table // Нет массивов для обработки или только одна строка
	}

	// Создаем новые строки
	var newRows []string
	
	// Копируем строки до строки с данными
	for i := 0; i < dataRowIndex; i++ {
		newRows = append(newRows, rows[i])
	}
	
	// Добавляем расширенные строки
	newRows = append(newRows, expandedRows...)
	
	// Копируем оставшиеся строки
	for i := dataRowIndex + 1; i < len(rows); i++ {
		newRows = append(newRows, rows[i])
	}

	// Заменяем содержимое таблицы
	newTable := table
	oldRowsStr := strings.Join(rows, "")
	newRowsStr := strings.Join(newRows, "")
	newTable = strings.Replace(newTable, oldRowsStr, newRowsStr, 1)
	
	return newTable
}

// expandRowsWithNestedArrays расширяет строку с учетом всех вложенных массивов и объединением ячеек
func (p *DocxProcessor) expandRowsWithNestedArrays(row string) []string {
	placeholderRegex := regexp.MustCompile(`\{([^}]+)\}`)
	matches := placeholderRegex.FindAllStringSubmatch(row, -1)
	
	if len(matches) == 0 {
		return []string{row}
	}

	// Анализируем структуру массивов для определения объединений
	arrayStructure := p.analyzeArrayStructure(matches)
	if len(arrayStructure.levels) == 0 {
		return []string{row}
	}

	// Генерируем строки с учетом объединения ячеек
	return p.generateRowsWithMerging(row, arrayStructure)
}

// ArrayStructure описывает структуру вложенных массивов
type ArrayStructure struct {
	levels []ArrayLevel
	totalRows int
}

// ArrayLevel описывает уровень массива
type ArrayLevel struct {
	path string
	size int
	keys []string // ключи, которые относятся к этому уровню
}

// analyzeArrayStructure анализирует структуру массивов в плейсхолдерах
func (p *DocxProcessor) analyzeArrayStructure(matches [][]string) ArrayStructure {
	structure := ArrayStructure{}
	levelMap := make(map[string]*ArrayLevel)
	
	// Анализируем каждый плейсхолдер
	for _, match := range matches {
		key := match[1]
		parts := strings.Split(key, ".")
		
		// Ищем все уровни массивов в пути к ключу
		currentPath := ""
		var current interface{} = p.data
		
		for i, part := range parts {
			if currentPath != "" {
				currentPath += "."
			}
			currentPath += part
			
			if m, ok := current.(map[string]interface{}); ok {
				if value, exists := m[part]; exists {
					if arr, isArray := value.([]interface{}); isArray && i < len(parts)-1 {
						// Это промежуточный массив
						if level, exists := levelMap[currentPath]; exists {
							if !contains(level.keys, key) {
								level.keys = append(level.keys, key)
							}
						} else {
							levelMap[currentPath] = &ArrayLevel{
								path: currentPath,
								size: len(arr),
								keys: []string{key},
							}
						}
						
						// Переходим к первому элементу для продолжения анализа
						if len(arr) > 0 {
							current = arr[0]
						}
					} else {
						current = value
					}
				}
			} else if arr, isArray := current.([]interface{}); isArray && len(arr) > 0 {
				// Мы в массиве, берем первый элемент
				if m, ok := arr[0].(map[string]interface{}); ok {
					if value, exists := m[part]; exists {
						if subArr, isSubArray := value.([]interface{}); isSubArray && i < len(parts)-1 {
							if level, exists := levelMap[currentPath]; exists {
								if !contains(level.keys, key) {
									level.keys = append(level.keys, key)
								}
							} else {
								levelMap[currentPath] = &ArrayLevel{
									path: currentPath,
									size: len(subArr),
									keys: []string{key},
								}
							}
							
							if len(subArr) > 0 {
								current = subArr[0]
							}
						} else {
							current = value
						}
					}
				}
			}
		}
	}
	
	// Преобразуем в упорядоченный список уровней
	for _, level := range levelMap {
		structure.levels = append(structure.levels, *level)
	}
	
	// Сортируем уровни по глубине (количество точек в пути)
	for i := 0; i < len(structure.levels); i++ {
		for j := i + 1; j < len(structure.levels); j++ {
			if strings.Count(structure.levels[i].path, ".") > strings.Count(structure.levels[j].path, ".") {
				structure.levels[i], structure.levels[j] = structure.levels[j], structure.levels[i]
			}
		}
	}
	
	// Вычисляем общее количество строк
	structure.totalRows = p.calculateTotalRows(structure.levels)
	
	return structure
}

// calculateTotalRows вычисляет общее количество строк для всех комбинаций
func (p *DocxProcessor) calculateTotalRows(levels []ArrayLevel) int {
	if len(levels) == 0 {
		return 1
	}
	
	// Для каждого элемента верхнего уровня считаем количество вложенных элементов
	total := 0
	firstLevel := levels[0]
	
	// Получаем массив верхнего уровня
	arr := p.getArrayByPath(firstLevel.path)
	
	for i := 0; i < len(arr); i++ {
		if len(levels) == 1 {
			total++
		} else {
			// Рекурсивно считаем для вложенных уровней
			subTotal := p.calculateSubRows(arr[i], levels[1:], firstLevel.path, i)
			total += subTotal
		}
	}
	
	return total
}

// calculateSubRows рекурсивно считает количество строк для вложенных уровней
func (p *DocxProcessor) calculateSubRows(parentItem interface{}, remainingLevels []ArrayLevel, parentPath string, parentIndex int) int {
	if len(remainingLevels) == 0 {
		return 1
	}
	
	currentLevel := remainingLevels[0]
	
	// Получаем массив для текущего уровня
	relativePath := strings.TrimPrefix(currentLevel.path, parentPath+".")
	subArr := p.getNestedArray(parentItem, relativePath)
	
	total := 0
	for i := 0; i < len(subArr); i++ {
		if len(remainingLevels) == 1 {
			total++
		} else {
			subTotal := p.calculateSubRows(subArr[i], remainingLevels[1:], currentLevel.path, i)
			total += subTotal
		}
	}
	
	return total
}

// generateRowsWithMerging генерирует строки с объединением ячеек
func (p *DocxProcessor) generateRowsWithMerging(templateRow string, structure ArrayStructure) []string {
	var result []string
	
	if len(structure.levels) == 0 {
		return []string{templateRow}
	}
	
	// Генерируем все комбинации индексов
	combinations := p.generateCombinations(structure.levels)
	
	for rowIndex, combination := range combinations {
		newRow := p.createRowWithMerging(templateRow, combination, structure, rowIndex)
		result = append(result, newRow)
	}
	
	return result
}

// RowCombination представляет комбинацию индексов для одной строки
type RowCombination struct {
	indices map[string]int
	keyValues map[string]string
}

// generateCombinations генерирует все возможные комбинации индексов
func (p *DocxProcessor) generateCombinations(levels []ArrayLevel) []RowCombination {
	var result []RowCombination
	
	if len(levels) == 0 {
		return result
	}
	
	// Получаем массив первого уровня
	firstArr := p.getArrayByPath(levels[0].path)
	
	for i := 0; i < len(firstArr); i++ {
		if len(levels) == 1 {
			combination := RowCombination{
				indices: map[string]int{levels[0].path: i},
				keyValues: make(map[string]string),
			}
			result = append(result, combination)
		} else {
			// Рекурсивно генерируем для вложенных уровней
			subCombinations := p.generateSubCombinations(firstArr[i], levels[1:], levels[0].path, i)
			for _, subComb := range subCombinations {
				// Добавляем индекс текущего уровня
				subComb.indices[levels[0].path] = i
				result = append(result, subComb)
			}
		}
	}
	
	return result
}

// generateSubCombinations рекурсивно генерирует комбинации для вложенных уровней
func (p *DocxProcessor) generateSubCombinations(parentItem interface{}, remainingLevels []ArrayLevel, parentPath string, parentIndex int) []RowCombination {
	var result []RowCombination
	
	if len(remainingLevels) == 0 {
		return []RowCombination{{
			indices: make(map[string]int),
			keyValues: make(map[string]string),
		}}
	}
	
	currentLevel := remainingLevels[0]
	relativePath := strings.TrimPrefix(currentLevel.path, parentPath+".")
	subArr := p.getNestedArray(parentItem, relativePath)
	
	for i := 0; i < len(subArr); i++ {
		if len(remainingLevels) == 1 {
			combination := RowCombination{
				indices: map[string]int{currentLevel.path: i},
				keyValues: make(map[string]string),
			}
			result = append(result, combination)
		} else {
			subCombinations := p.generateSubCombinations(subArr[i], remainingLevels[1:], currentLevel.path, i)
			for _, subComb := range subCombinations {
				subComb.indices[currentLevel.path] = i
				result = append(result, subComb)
			}
		}
	}
	
	return result
}

// createRowWithMerging создает строку с учетом объединения ячеек
func (p *DocxProcessor) createRowWithMerging(templateRow string, combination RowCombination, structure ArrayStructure, rowIndex int) string {
	placeholderRegex := regexp.MustCompile(`\{([^}]+)\}`)
	
	return placeholderRegex.ReplaceAllStringFunc(templateRow, func(match string) string {
		key := strings.Trim(match, "{}")
		
		// Определяем, к какому уровню относится этот ключ
		keyLevel := p.getKeyLevel(key, structure.levels)
		
		if keyLevel == -1 {
			// Обычный ключ без массивов
			return p.getValue(key)
		}
		
		// Проверяем, нужно ли объединить эту ячейку
		if p.shouldMergeCell(key, keyLevel, combination, rowIndex, structure) {
			// Возвращаем значение или пустую строку для объединенной ячейки
			if p.isFirstRowForLevel(keyLevel, combination, rowIndex, structure) {
				return p.getValueForCombination(key, combination)
			} else {
				// Для объединенных ячеек возвращаем специальный маркер
				return p.createMergedCell()
			}
		}
		
		// Обычная замена без объединения
		return p.getValueForCombination(key, combination)
	})
}

// getKeyLevel определяет, к какому уровню массива относится ключ
func (p *DocxProcessor) getKeyLevel(key string, levels []ArrayLevel) int {
	for i, level := range levels {
		if contains(level.keys, key) {
			return i
		}
	}
	return -1
}

// shouldMergeCell определяет, нужно ли объединить ячейку
func (p *DocxProcessor) shouldMergeCell(key string, keyLevel int, combination RowCombination, rowIndex int, structure ArrayStructure) bool {
	// Ячейки объединяются для уровней выше текущего самого глубокого
	if keyLevel == -1 || keyLevel >= len(structure.levels)-1 {
		return false
	}
	
	// Проверяем, есть ли более глубокие уровни с разными значениями
	for i := keyLevel + 1; i < len(structure.levels); i++ {
		if len(structure.levels[i].keys) > 0 {
			return true
		}
	}
	
	return false
}

// isFirstRowForLevel проверяет, является ли это первой строкой для данного уровня
func (p *DocxProcessor) isFirstRowForLevel(keyLevel int, combination RowCombination, rowIndex int, structure ArrayStructure) bool {
	// Упрощенная логика - показываем значение только если все индексы более глубоких уровней равны 0
	for i := keyLevel + 1; i < len(structure.levels); i++ {
		level := structure.levels[i]
		if index, exists := combination.indices[level.path]; exists && index != 0 {
			return false
		}
	}
	return true
}

// createMergedCell создает XML для объединенной ячейки
func (p *DocxProcessor) createMergedCell() string {
	// Возвращаем пустое содержимое для объединенной ячейки
	// В DOCX объединение происходит через специальные атрибуты
	return ""
}

// getValueForCombination получает значение для конкретной комбинации индексов
func (p *DocxProcessor) getValueForCombination(key string, combination RowCombination) string {
	parts := strings.Split(key, ".")
	var current interface{} = p.data
	
	for i, part := range parts {
		if m, ok := current.(map[string]interface{}); ok {
			if value, exists := m[part]; exists {
				if arr, isArray := value.([]interface{}); isArray {
					// Находим соответствующий индекс для этого массива
					currentPath := strings.Join(parts[:i+1], ".")
					if index, exists := combination.indices[currentPath]; exists && index < len(arr) {
						current = arr[index]
					} else if len(arr) > 0 {
						current = arr[0]
					} else {
						return ""
					}
				} else {
					current = value
				}
			} else {
				return ""
			}
		} else {
			return ""
		}
	}
	
	return p.convertToString(current)
}

// Вспомогательные функции
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func (p *DocxProcessor) getArrayByPath(path string) []interface{} {
	parts := strings.Split(path, ".")
	var current interface{} = p.data
	
	for _, part := range parts {
		if m, ok := current.(map[string]interface{}); ok {
			if value, exists := m[part]; exists {
				current = value
			} else {
				return nil
			}
		}
	}
	
	if arr, ok := current.([]interface{}); ok {
		return arr
	}
	return nil
}

func (p *DocxProcessor) getNestedArray(parent interface{}, relativePath string) []interface{} {
	if relativePath == "" {
		if arr, ok := parent.([]interface{}); ok {
			return arr
		}
		return nil
	}
	
	parts := strings.Split(relativePath, ".")
	current := parent
	
	for _, part := range parts {
		if m, ok := current.(map[string]interface{}); ok {
			if value, exists := m[part]; exists {
				current = value
			} else {
				return nil
			}
		}
	}
	
	if arr, ok := current.([]interface{}); ok {
		return arr
	}
	return nil
}

// getValue получает значение по ключу
func (p *DocxProcessor) getValue(key string) string {
	parts := strings.Split(key, ".")
	
	var current interface{} = p.data
	for _, part := range parts {
		if m, ok := current.(map[string]interface{}); ok {
			if value, exists := m[part]; exists {
				current = value
			} else {
				return ""
			}
		} else {
			return ""
		}
	}
	
	return p.convertToString(current)
}

// convertToString конвертирует значение в строку
func (p *DocxProcessor) convertToString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

func main() {
	if len(os.Args) != 4 {
		fmt.Println("Использование: go run main.go <input.docx> <data.json> <output.docx>")
		fmt.Println("")
		fmt.Println("Пример JSON структуры:")
		fmt.Println(`{
  "client": {
    "Name": "ООО Рога и Копыта",
    "Address": "г. Москва, ул. Примерная, д. 1"
  },
  "items": [
    {"name": "Товар 1", "price": 100, "quantity": 2},
    {"name": "Товар 2", "price": 250, "quantity": 1}
  ],
  "total": 450
}`)
		fmt.Println("")
		fmt.Println("Плейсхолдеры в DOCX:")
		fmt.Println("- {client.Name} - будет заменен на 'ООО Рога и Копыта'")
		fmt.Println("- {items.name} - в таблице создаст строки для каждого элемента массива")
		os.Exit(1)
	}

	inputFile := os.Args[1]
	jsonFile := os.Args[2]
	outputFile := os.Args[3]

	// Читаем JSON данные
	jsonData, err := os.ReadFile(jsonFile)
	if err != nil {
		log.Fatalf("Ошибка чтения JSON файла: %v", err)
	}

	// Создаем процессор
	processor, err := NewDocxProcessor(jsonData)
	if err != nil {
		log.Fatalf("Ошибка создания процессора: %v", err)
	}

	// Обрабатываем файл
	if err := processor.ProcessFile(inputFile, outputFile); err != nil {
		log.Fatalf("Ошибка обработки файла: %v", err)
	}

	fmt.Printf("Файл успешно обработан: %s -> %s\n", inputFile, outputFile)
}