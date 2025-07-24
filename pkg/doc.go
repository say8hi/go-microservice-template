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
	"strings"
)

type Document struct {
	XMLName xml.Name `xml:"document"`
	Body    Body     `xml:"body"`
}

type Body struct {
	XMLName xml.Name `xml:"body"`
	Content []interface{}
}

type Paragraph struct {
	XMLName xml.Name `xml:"p"`
	Runs    []Run    `xml:"r"`
}

type Table struct {
	XMLName xml.Name  `xml:"tbl"`
	Rows    []TableRow `xml:"tr"`
}

type TableRow struct {
	XMLName xml.Name    `xml:"tr"`
	Cells   []TableCell `xml:"tc"`
}

type TableCell struct {
	XMLName    xml.Name    `xml:"tc"`
	Paragraphs []Paragraph `xml:"p"`
}

type Run struct {
	XMLName xml.Name `xml:"r"`
	Text    []Text   `xml:"t"`
}

type Text struct {
	XMLName xml.Name `xml:"t"`
	Value   string   `xml:",chardata"`
	Space   string   `xml:"space,attr,omitempty"`
}

func (b *Body) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for {
		token, err := d.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		switch se := token.(type) {
		case xml.StartElement:
			switch se.Name.Local {
			case "p":
				var p Paragraph
				if err := d.DecodeElement(&p, &se); err != nil {
					return err
				}
				b.Content = append(b.Content, p)
			case "tbl":
				var t Table
				if err := d.DecodeElement(&t, &se); err != nil {
					return err
				}
				b.Content = append(b.Content, t)
			default:
				d.Skip()
			}
		case xml.EndElement:
			if se.Name.Local == "body" {
				return nil
			}
		}
	}
	return nil
}

type DocxProcessor struct {
	data map[string]interface{}
}

func NewDocxProcessor(jsonData []byte) (*DocxProcessor, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return nil, err
	}
	return &DocxProcessor{data: data}, nil
}

func (dp *DocxProcessor) ProcessDocx(docxPath string) ([]byte, error) {
	reader, err := zip.OpenReader(docxPath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)

	for _, file := range reader.File {
		if file.Name == "word/document.xml" {
			processedContent, err := dp.processDocumentXML(file)
			if err != nil {
				return nil, err
			}
			
			fw, err := writer.Create(file.Name)
			if err != nil {
				return nil, err
			}
			fw.Write(processedContent)
		} else {
			fw, err := writer.Create(file.Name)
			if err != nil {
				return nil, err
			}
			
			fr, err := file.Open()
			if err != nil {
				return nil, err
			}
			
			io.Copy(fw, fr)
			fr.Close()
		}
	}

	writer.Close()
	return buf.Bytes(), nil
}

func (dp *DocxProcessor) processDocumentXML(file *zip.File) ([]byte, error) {
	fr, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer fr.Close()

	content, err := io.ReadAll(fr)
	if err != nil {
		return nil, err
	}

	var doc Document
	if err := xml.Unmarshal(content, &doc); err != nil {
		return nil, err
	}

	dp.processBody(&doc.Body)

	result, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}

	xmlHeader := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`
	return []byte(xmlHeader + "\n" + string(result)), nil
}

func (dp *DocxProcessor) processBody(body *Body) {
	newContent := []interface{}{}

	for _, item := range body.Content {
		switch v := item.(type) {
		case Paragraph:
			newContent = append(newContent, dp.processParagraph(v))
		case Table:
			processedTables := dp.processTable(v)
			for _, table := range processedTables {
				newContent = append(newContent, table)
			}
		}
	}

	body.Content = newContent
}

func (dp *DocxProcessor) processParagraph(p Paragraph) Paragraph {
	text := dp.extractTextFromParagraph(p)
	placeholders := dp.findPlaceholders(text)
	
	if len(placeholders) == 0 {
		return p
	}

	newText := text
	for _, placeholder := range placeholders {
		value := dp.getValueFromJSON(placeholder)
		newText = strings.ReplaceAll(newText, "{"+placeholder+"}", fmt.Sprintf("%v", value))
	}

	p.Runs = []Run{{Text: []Text{{Value: newText, Space: "preserve"}}}}
	return p
}

func (dp *DocxProcessor) processTable(table Table) []Table {
	if len(table.Rows) == 0 {
		return []Table{table}
	}

	headerRow := table.Rows[0]
	if len(table.Rows) == 1 {
		return []Table{table}
	}

	templateRow := table.Rows[1]
	rowText := dp.extractTextFromRow(templateRow)
	placeholders := dp.findPlaceholders(rowText)

	if len(placeholders) == 0 {
		return []Table{table}
	}

	arrayPlaceholder := dp.findArrayPlaceholder(placeholders)
	if arrayPlaceholder == "" {
		newTable := Table{XMLName: table.XMLName, Rows: []TableRow{headerRow}}
		processedRow := dp.processTableRow(templateRow, placeholders)
		newTable.Rows = append(newTable.Rows, processedRow)
		return []Table{newTable}
	}

	arrayData := dp.getArrayFromJSON(arrayPlaceholder)
	if len(arrayData) == 0 {
		return []Table{table}
	}

	newTable := Table{XMLName: table.XMLName, Rows: []TableRow{headerRow}}

	for _, item := range arrayData {
		newRow := dp.duplicateTableRow(templateRow)
		for i, cell := range newRow.Cells {
			cellText := dp.extractTextFromCell(cell)
			cellPlaceholders := dp.findPlaceholders(cellText)
			
			newCellText := cellText
			for _, placeholder := range cellPlaceholders {
				var value interface{}
				if strings.HasPrefix(placeholder, arrayPlaceholder+".") {
					subKey := strings.TrimPrefix(placeholder, arrayPlaceholder+".")
					value = dp.getValueFromMap(item, subKey)
				} else {
					value = dp.getValueFromJSON(placeholder)
				}
				newCellText = strings.ReplaceAll(newCellText, "{"+placeholder+"}", fmt.Sprintf("%v", value))
			}
			
			if len(newRow.Cells[i].Paragraphs) > 0 {
				newRow.Cells[i].Paragraphs[0].Runs = []Run{{Text: []Text{{Value: newCellText, Space: "preserve"}}}}
			}
		}
		newTable.Rows = append(newTable.Rows, newRow)
	}

	return []Table{newTable}
}

func (dp *DocxProcessor) processTableRow(row TableRow, placeholders []string) TableRow {
	for i, cell := range row.Cells {
		cellText := dp.extractTextFromCell(cell)
		newCellText := cellText
		
		for _, placeholder := range placeholders {
			value := dp.getValueFromJSON(placeholder)
			newCellText = strings.ReplaceAll(newCellText, "{"+placeholder+"}", fmt.Sprintf("%v", value))
		}
		
		if len(row.Cells[i].Paragraphs) > 0 {
			row.Cells[i].Paragraphs[0].Runs = []Run{{Text: []Text{{Value: newCellText, Space: "preserve"}}}}
		}
	}
	return row
}

func (dp *DocxProcessor) duplicateTableRow(row TableRow) TableRow {
	newRow := TableRow{XMLName: row.XMLName}
	for _, cell := range row.Cells {
		newCell := TableCell{XMLName: cell.XMLName}
		for _, p := range cell.Paragraphs {
			newP := Paragraph{XMLName: p.XMLName}
			for _, run := range p.Runs {
				newRun := Run{XMLName: run.XMLName}
				for _, text := range run.Text {
					newText := Text{XMLName: text.XMLName, Value: text.Value, Space: text.Space}
					newRun.Text = append(newRun.Text, newText)
				}
				newP.Runs = append(newP.Runs, newRun)
			}
			newCell.Paragraphs = append(newCell.Paragraphs, newP)
		}
		newRow.Cells = append(newRow.Cells, newCell)
	}
	return newRow
}

func (dp *DocxProcessor) extractTextFromParagraph(p Paragraph) string {
	var text strings.Builder
	for _, run := range p.Runs {
		for _, t := range run.Text {
			text.WriteString(t.Value)
		}
	}
	return text.String()
}

func (dp *DocxProcessor) extractTextFromRow(row TableRow) string {
	var text strings.Builder
	for _, cell := range row.Cells {
		text.WriteString(dp.extractTextFromCell(cell))
		text.WriteString(" ")
	}
	return text.String()
}

func (dp *DocxProcessor) extractTextFromCell(cell TableCell) string {
	var text strings.Builder
	for _, p := range cell.Paragraphs {
		text.WriteString(dp.extractTextFromParagraph(p))
	}
	return text.String()
}

func (dp *DocxProcessor) findPlaceholders(text string) []string {
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

func (dp *DocxProcessor) findArrayPlaceholder(placeholders []string) string {
	for _, placeholder := range placeholders {
		parts := strings.Split(placeholder, ".")
		if len(parts) > 1 {
			arrayKey := parts[0]
			if value, exists := dp.data[arrayKey]; exists {
				if _, isArray := value.([]interface{}); isArray {
					return arrayKey
				}
			}
		}
	}
	return ""
}

func (dp *DocxProcessor) getValueFromJSON(key string) interface{} {
	parts := strings.Split(key, ".")
	current := dp.data
	
	for i, part := range parts {
		if i == len(parts)-1 {
			if val, exists := current[part]; exists {
				return val
			}
		} else {
			if val, exists := current[part]; exists {
				if nextMap, ok := val.(map[string]interface{}); ok {
					current = nextMap
				} else {
					return ""
				}
			} else {
				return ""
			}
		}
	}
	return ""
}

func (dp *DocxProcessor) getArrayFromJSON(key string) []map[string]interface{} {
	if value, exists := dp.data[key]; exists {
		if array, ok := value.([]interface{}); ok {
			var result []map[string]interface{}
			for _, item := range array {
				if mapItem, ok := item.(map[string]interface{}); ok {
					result = append(result, mapItem)
				}
			}
			return result
		}
	}
	return nil
}

func (dp *DocxProcessor) getValueFromMap(data map[string]interface{}, key string) interface{} {
	parts := strings.Split(key, ".")
	current := data
	
	for i, part := range parts {
		if i == len(parts)-1 {
			if val, exists := current[part]; exists {
				return val
			}
		} else {
			if val, exists := current[part]; exists {
				if nextMap, ok := val.(map[string]interface{}); ok {
					current = nextMap
				} else {
					return ""
				}
			} else {
				return ""
			}
		}
	}
	return ""
}

func SaveDocx(data []byte, outputPath string) error {
	return os.WriteFile(outputPath, data, 0644)
}

func main() {
	jsonData := []byte(`{
		"client": {
			"name": "John Doe",
			"email": "john@example.com"
		},
		"items": [
			{
				"name": "Product 1",
				"type": [
					{"name": "Type A", "price": 100},
					{"name": "Type B", "price": 150}
				]
			},
			{
				"name": "Product 2",
				"type": [
					{"name": "Type C", "price": 200}
				]
			}
		]
	}`)

	processor, err := NewDocxProcessor(jsonData)
	if err != nil {
		panic(err)
	}

	// Способ 1: Получить байты и сохранить отдельно
	result, err := processor.ProcessDocx("template.docx")
	if err != nil {
		panic(err)
	}

	err = SaveDocx(result, "output.docx")
	if err != nil {
		panic(err)
	}

	// Способ 2: Обработать и сохранить одной функцией
	err = processor.ProcessDocxToFile("template.docx", "output2.docx")
	if err != nil {
		panic(err)
	}

	fmt.Println("Файлы успешно созданы: output.docx и output2.docx")
}