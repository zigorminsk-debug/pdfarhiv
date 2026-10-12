package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// PageRange — диапазон страниц (1-based, включительно).
type PageRange struct {
	First int
	Last  int
}

// ParseRanges разбирает строку вида "1-3,5,7-" при заданном числе страниц документа.
func ParseRanges(spec string, maxPages int) ([]PageRange, error) {
	spec = strings.ReplaceAll(strings.TrimSpace(spec), " ", "")
	if spec == "" {
		return nil, fmt.Errorf("Укажите диапазоны, например: 1-3,5,7-10")
	}

	var result []PageRange
	for _, token := range strings.Split(spec, ",") {
		if token == "" {
			continue
		}
		var start, end int
		if a, b, hasDash := strings.Cut(token, "-"); hasDash {
			s, err := strconv.Atoi(a)
			if err != nil {
				return nil, fmt.Errorf("Не понял диапазон: «%s»", token)
			}
			start = s
			if b == "" {
				end = maxPages
			} else {
				e, err := strconv.Atoi(b)
				if err != nil {
					return nil, fmt.Errorf("Не понял диапазон: «%s»", token)
				}
				end = e
			}
		} else {
			s, err := strconv.Atoi(token)
			if err != nil {
				return nil, fmt.Errorf("Не понял диапазон: «%s»", token)
			}
			start = s
			end = s
		}

		if start < 1 || end < start {
			return nil, fmt.Errorf("Некорректный диапазон: «%s» (страницы с 1, начало ≤ конца)", token)
		}
		if start > maxPages {
			return nil, fmt.Errorf("Диапазон «%s» за пределами документа (всего %d стр.)", token, maxPages)
		}
		if end > maxPages {
			end = maxPages
		}
		result = append(result, PageRange{First: start, Last: end})
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("Не найдено ни одного диапазона")
	}
	return result, nil
}

// Chunks нарезает документ на порции по step страниц: 7 стр. по 3 -> (1-3),(4-6),(7-7).
func Chunks(total, step int) []PageRange {
	if step < 1 {
		step = 1
	}
	var result []PageRange
	for i := 1; i <= total; i += step {
		last := i + step - 1
		if last > total {
			last = total
		}
		result = append(result, PageRange{First: i, Last: last})
	}
	return result
}

// EachPage возвращает диапазоны по одной странице.
func EachPage(total int) []PageRange {
	result := make([]PageRange, 0, total)
	for i := 1; i <= total; i++ {
		result = append(result, PageRange{First: i, Last: i})
	}
	return result
}

// UniquePath возвращает незанятый путь, добавляя (1), (2)..., если файл уже существует.
func UniquePath(p string) string {
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return p
	}
	dir := filepath.Dir(p)
	base := filepath.Base(p)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; i < 1000; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%s_%d%s", stem, os.Getpid(), ext))
}

// FormatSize форматирует размер в байтах в человекочитаемый вид.
func FormatSize(bytes int64) string {
	b := float64(bytes)
	units := []string{"Б", "КБ", "МБ", "ГБ"}
	u := 0
	for b >= 1024 && u < len(units)-1 {
		b /= 1024
		u++
	}
	return fmt.Sprintf("%.1f %s", b, units[u])
}
