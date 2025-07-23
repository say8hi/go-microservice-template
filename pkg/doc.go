package main

import (
    "fmt"
    "log"
    "os"
    "regexp"
    "strings"

    "baliance.com/gooxml/document"
    "github.com/tidwall/gjson"
)

var placeholderRegex = regexp.MustCompile(`\{([a-zA-Z0-9\._]+)\}`)

func main() {
    jsonData, err := os.ReadFile("data.json")
    if err != nil {
        log.Fatal("Ошибка чтения JSON:", err)
    }

    doc, err := document.Open("template.docx")
    if err != nil {
        log.Fatal("Ошибка открытия шаблона DOCX:", err)
    }

    // Простой текст
    processParagraphs(doc, jsonData)

    // Таблицы
    processTables(doc, jsonData)

    if err := doc.SaveToFile("output.docx"); err != nil {
        log.Fatal("Ошибка сохранения:", err)
    }

    fmt.Println("Файл output.docx успешно создан.")
}

func processParagraphs(doc *document.Document, jsonData []byte) {
    for _, para := range doc.Paragraphs() {
        for _, run := range para.Runs() {
            text := run.Text()
            run.ClearContent()
            run.AddText(replacePlaceholders(text, jsonData))
        }
    }
}

func processTables(doc *document.Document, jsonData []byte) {
    for _, tbl := range doc.Tables() {
        for i := 0; i < len(tbl.Rows()); i++ {
            row := tbl.Rows()[i]
            placeholders := collectRowPlaceholders(row)
            if len(placeholders) == 0 {
                continue
            }

            arrayKey := getArrayKey(placeholders)
            if arrayKey == "" {
                replaceRow(row, jsonData)
                continue
            }

            array := gjson.GetBytes(jsonData, arrayKey)
            if !array.IsArray() {
                continue
            }

            tbl.RemoveRow(i)
            for j := len(array.Array()) - 1; j >= 0; j-- {
                item := array.Array()[j]

                nestedKey := findNestedArrayKey(item)
                nested := gjson.Get(item.Raw, nestedKey)

                if nested.Exists() && nested.IsArray() {
                    for k := len(nested.Array()) - 1; k >= 0; k-- {
                        merged := mergeNested(item, nested.Array()[k], nestedKey)
                        newRow := tbl.InsertRowAfter(i)
                        fillRowFromTemplate(newRow, row, merged)
                    }
                } else {
                    newRow := tbl.InsertRowAfter(i)
                    fillRowFromTemplate(newRow, row, item)
                }
            }
            break
        }
    }
}

func collectRowPlaceholders(row document.Row) []string {
    var result []string
    for _, cell := range row.Cells() {
        for _, para := range cell.Paragraphs() {
            for _, run := range para.Runs() {
                matches := placeholderRegex.FindAllStringSubmatch(run.Text(), -1)
                for _, match := range matches {
                    result = append(result, match[1])
                }
            }
        }
    }
    return result
}

func getArrayKey(paths []string) string {
    for _, path := range paths {
        parts := strings.Split(path, ".")
        if len(parts) > 0 {
            return parts[0]
        }
    }
    return ""
}

func findNestedArrayKey(obj gjson.Result) string {
    for k, v := range obj.Map() {
        if v.IsArray() {
            return k
        }
    }
    return ""
}

func mergeNested(parent, child gjson.Result, nestedKey string) gjson.Result {
    combined := make(map[string]interface{})
    for k, v := range parent.Map() {
        if k != nestedKey {
            combined[k] = v.Value()
        }
    }
    for k, v := range child.Map() {
        combined[nestedKey+"."+k] = v.Value()
    }

    jsonStr := "{"
    for k, v := range combined {
        valStr := gjson.Parse(fmt.Sprintf("%#v", v)).Raw
        jsonStr += fmt.Sprintf(`"%s":%s,`, k, valStr)
    }
    jsonStr = strings.TrimRight(jsonStr, ",") + "}"
    return gjson.Parse(jsonStr)
}

func replacePlaceholders(text string, jsonData []byte) string {
    return placeholderRegex.ReplaceAllStringFunc(text, func(match string) string {
        key := placeholderRegex.FindStringSubmatch(match)[1]
        val := gjson.GetBytes(jsonData, key)
        if val.Exists() {
            return val.String()
        }
        return match
    })
}

func fillRowFromTemplate(newRow, tmplRow document.Row, json gjson.Result) {
    for _, tmplCell := range tmplRow.Cells() {
        newCell := newRow.AddCell()
        for _, para := range tmplCell.Paragraphs() {
            newPara := newCell.AddParagraph()
            for _, run := range para.Runs() {
                text := run.Text()
                newText := replacePlaceholders(text, []byte(json.Raw))
                newRun := newPara.AddRun()
                newRun.AddText(newText)
            }
        }
    }
}

func replaceRow(row document.Row, jsonData []byte) {
    for _, cell := range row.Cells() {
        for _, para := range cell.Paragraphs() {
            for _, run := range para.Runs() {
                text := run.Text()
                run.ClearContent()
                run.AddText(replacePlaceholders(text, jsonData))
            }
        }
    }
}