package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

type Text struct {
	XMLName xml.Name `xml:"w:t"`
	Text    string   `xml:",chardata"`
}

type Run struct {
	XMLName xml.Name `xml:"w:r"`
	Texts   []Text   `xml:"w:t"`
}

type Paragraph struct {
	XMLName xml.Name `xml:"w:p"`
	Runs    []Run    `xml:"w:r"`
}

type Cell struct {
	XMLName   xml.Name    `xml:"w:tc"`
	Paragraph []Paragraph `xml:"w:p"`
	Raw       []byte      `xml:",innerxml"`
}

type Row struct {
	XMLName xml.Name `xml:"w:tr"`
	Cells   []Cell   `xml:"w:tc"`
	Raw     []byte   `xml:",innerxml"`
}

type Table struct {
	XMLName xml.Name `xml:"w:tbl"`
	Rows    []Row    `xml:"w:tr"`
}

type Body struct {
	XMLName xml.Name `xml:"w:body"`
	Tables  []Table  `xml:"w:tbl"`
}

type Document struct {
	XMLName xml.Name `xml:"w:document"`
	Body    Body     `xml:"w:body"`
}

func extractDocxXML(docxPath string) ([]byte, *zip.ReadCloser) {
	r, err := zip.OpenReader(docxPath)
	must(err)

	for _, f := range r.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			must(err)
			defer rc.Close()
			xmlBytes, err := io.ReadAll(rc)
			must(err)
			return xmlBytes, r
		}
	}
	panic("document.xml not found")
}

func saveDocxWithReplacedXML(r *zip.ReadCloser, newXML []byte, outputPath string) {
	out, err := os.Create(outputPath)
	must(err)
	defer out.Close()

	w := zip.NewWriter(out)
	defer w.Close()

	for _, f := range r.File {
		wr, err := w.Create(f.Name)
		must(err)

		if f.Name == "word/document.xml" {
			_, err = wr.Write(newXML)
			must(err)
		} else {
			rc, err := f.Open()
			must(err)
			defer rc.Close()
			io.Copy(wr, rc)
		}
	}
}

func getPlaceholders(text string) []string {
	re := regexp.MustCompile(`\{([^\}]+)\}`)
	matches := re.FindAllStringSubmatch(text, -1)
	var res []string
	for _, m := range matches {
		res = append(res, m[1])
	}
	return res
}

func getTextFromCell(c Cell) string {
	var out strings.Builder
	for _, p := range c.Paragraph {
		for _, r := range p.Runs {
			for _, t := range r.Texts {
				out.WriteString(t.Text)
			}
		}
	}
	return out.String()
}

func flattenArray(indexedPrefix string, obj map[string]interface{}, flat map[string]string) {
	for k, v := range obj {
		fullKey := indexedPrefix + "." + k
		switch vv := v.(type) {
		case string:
			flat[fullKey] = vv
		case float64:
			flat[fullKey] = strconv.FormatFloat(vv, 'f', -1, 64)
		case map[string]interface{}:
			flattenArray(fullKey, vv, flat)
		}
	}
}

func replacePlaceholders(text string, data map[string]string) string {
	for k, v := range data {
		text = strings.ReplaceAll(text, "{"+k+"}", v)
	}
	return text
}

func processTable(t Table, jsonData map[string]interface{}) []Row {
	var result []Row

	for _, row := range t.Rows {
		joinedRowText := ""
		for _, cell := range row.Cells {
			joinedRowText += getTextFromCell(cell)
		}

		placeholders := getPlaceholders(joinedRowText)
		if len(placeholders) > 0 && strings.HasPrefix(placeholders[0], "items") {
			itemsRaw, ok := jsonData["items"].([]interface{})
			if !ok {
				continue
			}
			for i, item := range itemsRaw {
				rowCopy := row
				flat := map[string]string{}
				if itemMap, ok := item.(map[string]interface{}); ok {
					flattenArray(fmt.Sprintf("items[%d]", i), itemMap, flat)
				}
				for ci, cell := range rowCopy.Cells {
					text := getTextFromCell(cell)
					newText := replacePlaceholders(text, flat)
					newCellXML := strings.Replace(string(cell.Raw), text, newText, 1)
					rowCopy.Cells[ci].Raw = []byte(newCellXML)
				}
				result = append(result, rowCopy)
			}
		} else {
			flat := map[string]string{}
			flattenArray("", jsonData, flat)
			rowCopy := row
			for ci, cell := range rowCopy.Cells {
				text := getTextFromCell(cell)
				newText := replacePlaceholders(text, flat)
				newCellXML := strings.Replace(string(cell.Raw), text, newText, 1)
				rowCopy.Cells[ci].Raw = []byte(newCellXML)
			}
			result = append(result, rowCopy)
		}
	}
	return result
}

func main() {
	docxPath := "template.docx"
	jsonPath := "data.json"
	outputPath := "output.docx"

	xmlBytes, reader := extractDocxXML(docxPath)
	defer reader.Close()

	var doc Document
	must(xml.Unmarshal(xmlBytes, &doc))

	jsonRaw, err := os.ReadFile(jsonPath)
	must(err)

	var jsonData map[string]interface{}
	must(json.Unmarshal(jsonRaw, &jsonData))

	var finalTables []string
	for _, tbl := range doc.Body.Tables {
		var buffer strings.Builder
		buffer.WriteString(`<w:tbl>`)

		rows := processTable(tbl, jsonData)
		for _, row := range rows {
			buffer.WriteString(`<w:tr>`)
			for _, cell := range row.Cells {
				buffer.WriteString(`<w:tc>`)
				buffer.Write(cell.Raw)
				buffer.WriteString(`</w:tc>`)
			}
			buffer.WriteString(`</w:tr>`)
		}

		buffer.WriteString(`</w:tbl>`)
		finalTables = append(finalTables, buffer.String())
	}

	finalXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:body>` +
		strings.Join(finalTables, "") +
		`</w:body></w:document>`

	saveDocxWithReplacedXML(reader, []byte(finalXML), outputPath)
	fmt.Println("✅ Done: saved to", outputPath)
}