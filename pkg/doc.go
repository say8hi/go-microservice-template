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

// WordDocument represents the main document structure
type WordDocument struct {
	XMLName xml.Name `xml:"document"`
	Body    Body     `xml:"body"`
}

// Body represents the document body
type Body struct {
	XMLName    xml.Name    `xml:"body"`
	Paragraphs []Paragraph `xml:"p"`
	Tables     []Table     `xml:"tbl"`
}

// Table represents a Word table
type Table struct {
	XMLName xml.Name    `xml:"tbl"`
	Rows    []TableRow  `xml:"tr"`
	Props   interface{} `xml:"tblPr"`
}

// TableRow represents a table row
type TableRow struct {
	XMLName xml.Name    `xml:"tr"`
	Cells   []TableCell `xml:"tc"`
	Props   interface{} `xml:"trPr"`
}

// TableCell represents a table cell
type TableCell struct {
	XMLName    xml.Name    `xml:"tc"`
	Paragraphs []Paragraph `xml:"p"`
	Props      interface{} `xml:"tcPr"`
}

// Paragraph represents a paragraph
type Paragraph struct {
	XMLName xml.Name `xml:"p"`
	Runs    []Run    `xml:"r"`
	Props   interface{} `xml:"pPr"`
}

// Run represents a text run
type Run struct {
	XMLName xml.Name `xml:"r"`
	Texts   []Text   `xml:"t"`
	Props   interface{} `xml:"rPr"`
}

// Text represents text content
type Text struct {
	XMLName xml.Name `xml:"t"`
	Space   string   `xml:"space,attr,omitempty"`
	Content string   `xml:",chardata"`
}

// PlaceholderProcessor handles placeholder replacement
type PlaceholderProcessor struct {
	data interface{}
}

// NewPlaceholderProcessor creates a new processor
func NewPlaceholderProcessor(jsonData []byte) (*PlaceholderProcessor, error) {
	var data interface{}
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %v", err)
	}
	
	return &PlaceholderProcessor{data: data}, nil
}

// ProcessDocument processes the entire document
func (pp *PlaceholderProcessor) ProcessDocument(doc *WordDocument) error {
	// Process paragraphs
	for i := range doc.Body.Paragraphs {
		if err := pp.processParagraph(&doc.Body.Paragraphs[i]); err != nil {
			return err
		}
	}
	
	// Process tables
	for i := range doc.Body.Tables {
		if err := pp.processTable(&doc.Body.Tables[i]); err != nil {
			return err
		}
	}
	
	return nil
}

// processTable handles table processing with array expansion
func (pp *PlaceholderProcessor) processTable(table *Table) error {
	if len(table.Rows) == 0 {
		return nil
	}
	
	// Find placeholders in the first row to determine array expansions
	arrayPlaceholders := pp.findArrayPlaceholders(table.Rows[0])
	
	if len(arrayPlaceholders) == 0 {
		// No arrays, process normally
		for i := range table.Rows {
			if err := pp.processTableRow(&table.Rows[i]); err != nil {
				return err
			}
		}
		return nil
	}
	
	// Expand rows based on arrays
	return pp.expandTableRows(table, arrayPlaceholders)
}

// findArrayPlaceholders finds placeholders that reference arrays
func (pp *PlaceholderProcessor) findArrayPlaceholders(row TableRow) map[int]string {
	arrayPlaceholders := make(map[int]string)
	
	for cellIdx, cell := range row.Cells {
		placeholders := pp.extractPlaceholdersFromCell(cell)
		for _, placeholder := range placeholders {
			if pp.isArrayPlaceholder(placeholder) {
				arrayPlaceholders[cellIdx] = placeholder
			}
		}
	}
	
	return arrayPlaceholders
}

// extractPlaceholdersFromCell extracts all placeholders from a cell
func (pp *PlaceholderProcessor) extractPlaceholdersFromCell(cell TableCell) []string {
	var placeholders []string
	
	for _, paragraph := range cell.Paragraphs {
		text := pp.reconstructTextFromParagraph(paragraph)
		found := pp.extractPlaceholders(text)
		placeholders = append(placeholders, found...)
	}
	
	return placeholders
}

// reconstructTextFromParagraph reconstructs text from paragraph runs
func (pp *PlaceholderProcessor) reconstructTextFromParagraph(paragraph Paragraph) string {
	var text strings.Builder
	
	for _, run := range paragraph.Runs {
		for _, t := range run.Texts {
			text.WriteString(t.Content)
		}
	}
	
	return text.String()
}

// extractPlaceholders extracts placeholders from text
func (pp *PlaceholderProcessor) extractPlaceholders(text string) []string {
	re := regexp.MustCompile(`\{([^}]+)\}`)
	matches := re.FindAllStringSubmatch(text, -1)
	
	var placeholders []string
	for _, match := range matches {
		if len(match) > 1 {
			placeholders = append(placeholders, match[1])
		}
	}
	
	return placeholders
}

// isArrayPlaceholder checks if a placeholder references an array
func (pp *PlaceholderProcessor) isArrayPlaceholder(placeholder string) bool {
	value := pp.getValueByPath(placeholder)
	if value == nil {
		return false
	}
	
	v := reflect.ValueOf(value)
	return v.Kind() == reflect.Slice || v.Kind() == reflect.Array
}

// expandTableRows expands table rows based on array data
func (pp *PlaceholderProcessor) expandTableRows(table *Table, arrayPlaceholders map[int]string) error {
	if len(table.Rows) == 0 {
		return nil
	}
	
	templateRow := table.Rows[0]
	var newRows []TableRow
	
	// Determine the maximum number of rows needed
	maxRows := pp.getMaxArrayLength(arrayPlaceholders)
	
	for rowIdx := 0; rowIdx < maxRows; rowIdx++ {
		newRow := pp.cloneTableRow(templateRow)
		
		// Process each cell with array context
		for cellIdx, cell := range newRow.Cells {
			if arrayPlaceholder, exists := arrayPlaceholders[cellIdx]; exists {
				// This cell has an array placeholder
				pp.processCellWithArrayIndex(&newRow.Cells[cellIdx], arrayPlaceholder, rowIdx)
			} else {
				// Regular cell processing
				pp.processTableCell(&newRow.Cells[cellIdx])
			}
		}
		
		newRows = append(newRows, newRow)
	}
	
	// Replace the original rows with expanded rows
	table.Rows = append(newRows, table.Rows[1:]...)
	
	return nil
}

// getMaxArrayLength gets the maximum length among all arrays
func (pp *PlaceholderProcessor) getMaxArrayLength(arrayPlaceholders map[int]string) int {
	maxLength := 0
	
	for _, placeholder := range arrayPlaceholders {
		value := pp.getValueByPath(placeholder)
		if value != nil {
			v := reflect.ValueOf(value)
			if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
				if v.Len() > maxLength {
					maxLength = v.Len()
				}
			}
		}
	}
	
	return maxLength
}

// cloneTableRow creates a deep copy of a table row
func (pp *PlaceholderProcessor) cloneTableRow(row TableRow) TableRow {
	newRow := TableRow{
		XMLName: row.XMLName,
		Props:   row.Props,
	}
	
	for _, cell := range row.Cells {
		newCell := TableCell{
			XMLName: cell.XMLName,
			Props:   cell.Props,
		}
		
		for _, paragraph := range cell.Paragraphs {
			newParagraph := Paragraph{
				XMLName: paragraph.XMLName,
				Props:   paragraph.Props,
			}
			
			for _, run := range paragraph.Runs {
				newRun := Run{
					XMLName: run.XMLName,
					Props:   run.Props,
				}
				
				for_, text := range run.Texts {
					newRun.Texts = append(newRun.Texts, Text{
						XMLName: text.XMLName,
						Space:   text.Space,
						Content: text.Content,
					})
				}
				
				newParagraph.Runs = append(newParagraph.Runs, newRun)
			}
			
			newCell.Paragraphs = append(newCell.Paragraphs, newParagraph)
		}
		
		newRow.Cells = append(newRow.Cells, newCell)
	}
	
	return newRow
}

// processCellWithArrayIndex processes a cell with array index context
func (pp *PlaceholderProcessor) processCellWithArrayIndex(cell *TableCell, arrayPlaceholder string, index int) {
	for i := range cell.Paragraphs {
		pp.processParagraphWithArrayIndex(&cell.Paragraphs[i], arrayPlaceholder, index)
	}
}

// processParagraphWithArrayIndex processes a paragraph with array index context  
func (pp *PlaceholderProcessor) processParagraphWithArrayIndex(paragraph *Paragraph, arrayPlaceholder string, index int) {
	// Reconstruct and process text
	fullText := pp.reconstructTextFromParagraph(*paragraph)
	processedText := pp.replaceArrayPlaceholders(fullText, arrayPlaceholder, index)
	
	// Update the paragraph with processed text
	pp.updateParagraphText(paragraph, processedText)
}

// replaceArrayPlaceholders replaces array placeholders with indexed values
func (pp *PlaceholderProcessor) replaceArrayPlaceholders(text, arrayPlaceholder string, index int) string {
	// Replace array placeholders
	re := regexp.MustCompile(`\{` + regexp.QuoteMeta(arrayPlaceholder) + `(?:\.([^}]+))?\}`)
	
	result := re.ReplaceAllStringFunc(text, func(match string) string {
		// Extract the full placeholder
		placeholder := strings.Trim(match, "{}")
		
		// Get array value
		arrayValue := pp.getValueByPath(arrayPlaceholder)
		if arrayValue == nil {
			return match
		}
		
		v := reflect.ValueOf(arrayValue)
		if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
			return match
		}
		
		if index >= v.Len() {
			return ""
		}
		
		itemValue := v.Index(index).Interface()
		
		// If it's just the array placeholder, return the item
		if placeholder == arrayPlaceholder {
			return pp.formatValue(itemValue)
		}
		
		// If it has a sub-field, get that field
		if strings.Contains(placeholder, ".") {
			parts := strings.Split(placeholder, ".")
			if len(parts) > 1 {
				subPath := strings.Join(parts[1:], ".")
				subValue := pp.getValueFromObject(itemValue, subPath)
				return pp.formatValue(subValue)
			}
		}
		
		return match
	})
	
	// Replace other placeholders normally
	return pp.replacePlaceholders(result)
}

// processTableRow processes a table row
func (pp *PlaceholderProcessor) processTableRow(row *TableRow) error {
	for i := range row.Cells {
		if err := pp.processTableCell(&row.Cells[i]); err != nil {
			return err
		}
	}
	return nil
}

// processTableCell processes a table cell
func (pp *PlaceholderProcessor) processTableCell(cell *TableCell) error {
	for i := range cell.Paragraphs {
		if err := pp.processParagraph(&cell.Paragraphs[i]); err != nil {
			return err
		}
	}
	return nil
}

// processParagraph processes a paragraph
func (pp *PlaceholderProcessor) processParagraph(paragraph *Paragraph) error {
	// Reconstruct text from all runs
	fullText := pp.reconstructTextFromParagraph(*paragraph)
	
	// Replace placeholders
	processedText := pp.replacePlaceholders(fullText)
	
	// Update paragraph with processed text
	pp.updateParagraphText(paragraph, processedText)
	
	return nil
}

// updateParagraphText updates paragraph with new text
func (pp *PlaceholderProcessor) updateParagraphText(paragraph *Paragraph, newText string) {
	// Clear existing runs and create a new one with the processed text
	paragraph.Runs = []Run{
		{
			XMLName: xml.Name{Local: "r"},
			Texts: []Text{
				{
					XMLName: xml.Name{Local: "t"},
					Content: newText,
					Space:   "preserve",
				},
			},
		},
	}
}

// replacePlaceholders replaces placeholders in text
func (pp *PlaceholderProcessor) replacePlaceholders(text string) string {
	re := regexp.MustCompile(`\{([^}]+)\}`)
	
	return re.ReplaceAllStringFunc(text, func(match string) string {
		placeholder := strings.Trim(match, "{}")
		value := pp.getValueByPath(placeholder)
		return pp.formatValue(value)
	})
}

// getValueByPath gets value by dot-separated path
func (pp *PlaceholderProcessor) getValueByPath(path string) interface{} {
	parts := strings.Split(path, ".")
	current := pp.data
	
	for _, part := range parts {
		current = pp.getValueFromObject(current, part)
		if current == nil {
			return nil
		}
	}
	
	return current
}

// getValueFromObject gets value from object by key
func (pp *PlaceholderProcessor) getValueFromObject(obj interface{}, key string) interface{} {
	if obj == nil {
		return nil
	}
	
	v := reflect.ValueOf(obj)
	
	// Handle interface{} and pointers
	for v.Kind() == reflect.Interface || v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	
	switch v.Kind() {
	case reflect.Map:
		mapValue := v.MapIndex(reflect.ValueOf(key))
		if !mapValue.IsValid() {
			return nil
		}
		return mapValue.Interface()
		
	case reflect.Struct:
		field := v.FieldByName(key)
		if !field.IsValid() {
			return nil
		}
		return field.Interface()
		
	default:
		return nil
	}
}

// formatValue formats a value for display
func (pp *PlaceholderProcessor) formatValue(value interface{}) string {
	if value == nil {
		return ""
	}
	
	v := reflect.ValueOf(value)
	
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'f', -1, 64)
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	default:
		return fmt.Sprintf("%v", value)
	}
}

// ProcessDocxFile processes a DOCX file
func ProcessDocxFile(inputPath, outputPath string, jsonData []byte) error {
	// Open the DOCX file
	reader, err := zip.OpenReader(inputPath)
	if err != nil {
		return fmt.Errorf("failed to open DOCX file: %v", err)
	}
	defer reader.Close()
	
	// Create output buffer
	var outputBuffer bytes.Buffer
	writer := zip.NewWriter(&outputBuffer)
	defer writer.Close()
	
	// Create processor
	processor, err := NewPlaceholderProcessor(jsonData)
	if err != nil {
		return err
	}
	
	// Process each file in the DOCX
	for _, file := range reader.File {
		if file.Name == "word/document.xml" {
			// Process the main document
			if err := processDocumentXML(file, writer, processor); err != nil {
				return err
			}
		} else {
			// Copy other files as-is
			if err := copyFile(file, writer); err != nil {
				return err
			}
		}
	}
	
	writer.Close()
	
	// Write output file
	return os.WriteFile(outputPath, outputBuffer.Bytes(), 0644)
}

// processDocumentXML processes the main document XML
func processDocumentXML(file *zip.File, writer *zip.Writer, processor *PlaceholderProcessor) error {
	// Read the document XML
	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	
	content, err := io.ReadAll(rc)
	if err != nil {
		return err
	}
	
	// Parse XML
	var doc WordDocument
	if err := xml.Unmarshal(content, &doc); err != nil {
		return fmt.Errorf("failed to parse document XML: %v", err)
	}
	
	// Process placeholders
	if err := processor.ProcessDocument(&doc); err != nil {
		return err
	}
	
	// Marshal back to XML
	output, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	
	// Add XML header
	xmlContent := xml.Header + string(output)
	
	// Write to output
	w, err := writer.Create(file.Name)
	if err != nil {
		return err
	}
	
	_, err = w.Write([]byte(xmlContent))
	return err
}

// copyFile copies a file from input to output zip
func copyFile(file *zip.File, writer *zip.Writer) error {
	// Create file in output zip
	w, err := writer.Create(file.Name)
	if err != nil {
		return err
	}
	
	// Open source file
	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	
	// Copy content
	_, err = io.Copy(w, rc)
	return err
}

func main() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: go run main.go <input.docx> <output.docx> <data.json>")
		os.Exit(1)
	}
	
	inputPath := os.Args[1]
	outputPath := os.Args[2]
	jsonPath := os.Args[3]
	
	// Read JSON data
	jsonData, err := os.ReadFile(jsonPath)
	if err != nil {
		log.Fatalf("Failed to read JSON file: %v", err)
	}
	
	// Process DOCX file
	if err := ProcessDocxFile(inputPath, outputPath, jsonData); err != nil {
		log.Fatalf("Failed to process DOCX file: %v", err)
	}
	
	fmt.Printf("Successfully processed %s -> %s\n", inputPath, outputPath)
}