package soap

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

// RedactedValue is substituted for credential-bearing diagnostic values.
const RedactedValue = "REDACTED"

// maxDiagnosticBytes bounds a sanitized diagnostic string so a hostile parser
// error or document fragment cannot produce an unbounded message.
const maxDiagnosticBytes = 512

var (
	doubleQuotedLooseSecret  = regexp.MustCompile(`(?is)\b(passworddigest|password|nonce|username|authorization|proxy-authorization)\s*=\s*"[^"]*$`)
	singleQuotedLooseSecret  = regexp.MustCompile(`(?is)\b(passworddigest|password|nonce|username|authorization|proxy-authorization)\s*=\s*'[^']*$`)
	unquotedLooseSecret      = regexp.MustCompile(`(?is)\b(passworddigest|password|nonce|username|authorization|proxy-authorization)\s*=\s*[^\s<>"']+$`)
	userinfoPattern          = regexp.MustCompile(`(?i)(https?://)[^/@\s]+@`)
	urlQueryPattern          = regexp.MustCompile(`(?i)(https?://[^\s?#]+)\?[^\s#<]*`)
	authorizationLinePattern = regexp.MustCompile(`(?im)(proxy-authorization|authorization)\s*:\s*[^\r\n<]*`)
)

var sensitiveXMLLocals = map[string]bool{
	"passworddigest":      true,
	"password":            true,
	"nonce":               true,
	"username":            true,
	"authorization":       true,
	"proxy-authorization": true,
}

// RedactXML removes credential material from XML and partial XML. It uses a
// bounded lexical scan rather than an XML parser so malformed and truncated
// input is handled fail closed. Once a sensitive element or attribute name has
// been recognized, an incomplete quote/start/end tag causes the uncertain tail
// to be replaced or dropped; it is never copied into the diagnostic.
func RedactXML(input []byte) []byte {
	// A response limit may split a multibyte rune. Normalize first so all index
	// operations and all returned diagnostics remain valid UTF-8.
	text := strings.ToValidUTF8(string(input), "")
	redacted := redactXMLLexically(text)
	return []byte(redactPlainText(redacted))
}

func redactXMLLexically(text string) string {
	var output strings.Builder
	for offset := 0; offset < len(text); {
		relative := strings.IndexByte(text[offset:], '<')
		if relative < 0 {
			output.WriteString(text[offset:])
			break
		}
		start := offset + relative
		output.WriteString(text[offset:start])
		nameStart := start + 1
		for nameStart < len(text) && isXMLSpace(text[nameStart]) {
			nameStart++
		}
		if nameStart >= len(text) {
			output.WriteByte('<')
			break
		}
		if text[nameStart] == '/' {
			// Closing tags contain no value. Copy a complete tag; an incomplete
			// one is safe to retain after invalid UTF-8 has been removed.
			end, nested, complete, _ := findStartTagEnd(text, nameStart+1)
			if complete {
				output.WriteString(text[start : end+1])
				offset = end + 1
				continue
			}
			if nested >= 0 {
				output.WriteString(text[start:nested])
				offset = nested
				continue
			}
			output.WriteString(text[start:])
			break
		}

		nameEnd := scanXMLName(text, nameStart)
		if nameEnd == nameStart {
			// Processing the next '<' separately catches credential-shaped XML
			// embedded in malformed markup, comments, or CDATA diagnostics.
			output.WriteByte('<')
			offset = start + 1
			continue
		}
		qualifiedName := text[nameStart:nameEnd]
		local := strings.ToLower(xmlLocalName(qualifiedName))
		end, nested, complete, selfClosing := findStartTagEnd(text, nameEnd)

		if sensitiveXMLLocals[local] {
			output.WriteByte('<')
			output.WriteString(qualifiedName)
			output.WriteByte('>')
			output.WriteString(RedactedValue)
			if !complete {
				// The opening tag is incomplete. Attributes may contain the value,
				// so discard the entire uncertain suffix.
				break
			}
			if selfClosing {
				output.WriteString("</")
				output.WriteString(qualifiedName)
				output.WriteByte('>')
				offset = end + 1
				continue
			}
			closingEnd, found := findSensitiveClosingTag(text, end+1, qualifiedName)
			if !found {
				// The value or end tag is truncated. Do not copy any of it.
				break
			}
			output.WriteString("</")
			output.WriteString(qualifiedName)
			output.WriteByte('>')
			offset = closingEnd + 1
			continue
		}

		if local == "usernametoken" {
			// UsernameToken start-tag attributes can themselves carry nonce-like
			// material. Preserve only the QName, even for well-formed input.
			output.WriteByte('<')
			output.WriteString(qualifiedName)
			if selfClosing {
				output.WriteString("/>")
			} else {
				output.WriteByte('>')
			}
			if !complete {
				break
			}
			offset = end + 1
			continue
		}

		segmentEnd := len(text)
		if complete {
			segmentEnd = end + 1
		} else if nested >= 0 {
			segmentEnd = nested
		}
		output.WriteString(redactTagAttributes(text[start:segmentEnd], nameEnd-start))
		if complete {
			offset = end + 1
			continue
		}
		if nested >= 0 {
			offset = nested
			continue
		}
		break
	}
	return output.String()
}

// redactTagAttributes redacts complete, malformed, quoted, unquoted, and
// truncated sensitive attributes in one start-tag fragment. nameEnd is the byte
// offset just after the element QName within fragment.
func redactTagAttributes(fragment string, nameEnd int) string {
	var output strings.Builder
	output.WriteString(fragment[:nameEnd])
	for index := nameEnd; index < len(fragment); {
		if isXMLSpace(fragment[index]) || fragment[index] == '/' || fragment[index] == '>' {
			output.WriteByte(fragment[index])
			index++
			continue
		}
		if fragment[index] == '<' {
			break
		}

		attributeStart := index
		attributeEnd := scanXMLName(fragment, index)
		if attributeEnd == attributeStart {
			output.WriteByte(fragment[index])
			index++
			continue
		}
		attributeName := fragment[attributeStart:attributeEnd]
		sensitive := sensitiveXMLLocals[strings.ToLower(xmlLocalName(attributeName))]
		output.WriteString(attributeName)
		index = attributeEnd
		for index < len(fragment) && isXMLSpace(fragment[index]) {
			output.WriteByte(fragment[index])
			index++
		}
		if index >= len(fragment) || fragment[index] != '=' {
			if sensitive {
				output.WriteString(`="` + RedactedValue + `"`)
				// A malformed sensitive assignment has no reliable value boundary.
				// Keep only a final tag terminator, never the uncertain bytes.
				if strings.HasSuffix(fragment, ">") {
					output.WriteByte('>')
				}
				return output.String()
			}
			continue
		}
		output.WriteByte('=')
		index++
		for index < len(fragment) && isXMLSpace(fragment[index]) {
			output.WriteByte(fragment[index])
			index++
		}
		if index >= len(fragment) {
			if sensitive {
				output.WriteString(`"` + RedactedValue + `"`)
			}
			return output.String()
		}

		quote := fragment[index]
		if quote == '\'' || quote == '"' {
			closing := strings.IndexByte(fragment[index+1:], quote)
			if sensitive {
				output.WriteByte(quote)
				output.WriteString(RedactedValue)
				output.WriteByte(quote)
				if closing < 0 {
					return output.String()
				}
				index += closing + 2
				continue
			}
			if closing < 0 {
				output.WriteString(fragment[index:])
				return output.String()
			}
			closing += index + 1
			output.WriteString(fragment[index : closing+1])
			index = closing + 1
			continue
		}

		valueEnd := index
		for valueEnd < len(fragment) && !isXMLSpace(fragment[valueEnd]) && fragment[valueEnd] != '>' && fragment[valueEnd] != '<' {
			valueEnd++
		}
		if sensitive {
			output.WriteByte('"')
			output.WriteString(RedactedValue)
			output.WriteByte('"')
		} else {
			output.WriteString(fragment[index:valueEnd])
		}
		index = valueEnd
	}
	return output.String()
}

func findStartTagEnd(text string, start int) (end, nested int, complete, selfClosing bool) {
	quote := byte(0)
	for index := start; index < len(text); index++ {
		character := text[index]
		if quote != 0 {
			if character == quote {
				quote = 0
			}
			continue
		}
		switch character {
		case '\'', '"':
			quote = character
		case '<':
			return -1, index, false, false
		case '>':
			before := index - 1
			for before >= start && isXMLSpace(text[before]) {
				before--
			}
			return index, -1, true, before >= start && text[before] == '/'
		}
	}
	return -1, -1, false, false
}

// findSensitiveClosingTag scans markup boundaries inside a sensitive element.
// It skips comments, CDATA, and processing instructions; tracks nested elements
// with the exact same QName; and rejects incomplete or same-local mismatched
// closers. A lexical ambiguity returns false so the caller drops the entire
// uncertain tail.
func findSensitiveClosingTag(text string, start int, qualifiedName string) (int, bool) {
	depth := 1
	for offset := start; offset < len(text); {
		relative := strings.IndexByte(text[offset:], '<')
		if relative < 0 {
			return 0, false
		}
		candidate := offset + relative

		switch {
		case strings.HasPrefix(text[candidate:], "<!--"):
			end := strings.Index(text[candidate+4:], "-->")
			if end < 0 {
				return 0, false
			}
			offset = candidate + 4 + end + 3
			continue
		case strings.HasPrefix(text[candidate:], "<![CDATA["):
			end := strings.Index(text[candidate+9:], "]]>")
			if end < 0 {
				return 0, false
			}
			offset = candidate + 9 + end + 3
			continue
		case strings.HasPrefix(text[candidate:], "<?"):
			end := strings.Index(text[candidate+2:], "?>")
			if end < 0 {
				return 0, false
			}
			offset = candidate + 2 + end + 2
			continue
		case strings.HasPrefix(text[candidate:], "<!"):
			// Declarations inside element content are invalid or complex enough
			// that resuming diagnostic output would not be fail closed.
			return 0, false
		}

		index := candidate + 1
		if index >= len(text) || isXMLSpace(text[index]) {
			return 0, false
		}
		closing := text[index] == '/'
		if closing {
			index++
			if index >= len(text) || isXMLSpace(text[index]) {
				return 0, false
			}
		}
		nameEnd := scanXMLName(text, index)
		if nameEnd == index {
			return 0, false
		}
		candidateName := text[index:nameEnd]
		end, nested, complete, selfClosing := findStartTagEnd(text, nameEnd)
		if !complete || nested >= 0 {
			return 0, false
		}

		if closing && strings.EqualFold(xmlLocalName(candidateName), xmlLocalName(qualifiedName)) && candidateName != qualifiedName {
			// XML end-tag names are case- and prefix-sensitive. A same-local
			// mismatch is a tempting false closer, so fail closed immediately.
			return 0, false
		}
		if candidateName == qualifiedName {
			if closing {
				for cursor := nameEnd; cursor < end; cursor++ {
					if !isXMLSpace(text[cursor]) {
						return 0, false
					}
				}
				depth--
				if depth == 0 {
					return end, true
				}
			} else if !selfClosing {
				depth++
			}
		}
		offset = end + 1
	}
	return 0, false
}

func scanXMLName(text string, start int) int {
	index := start
	for index < len(text) {
		character := text[index]
		if isXMLSpace(character) || character == '/' || character == '>' || character == '=' || character == '<' || character == '\'' || character == '"' {
			break
		}
		_, width := utf8.DecodeRuneInString(text[index:])
		if width == 0 {
			break
		}
		index += width
	}
	return index
}

func xmlLocalName(qualified string) string {
	if separator := strings.LastIndexByte(qualified, ':'); separator >= 0 {
		return qualified[separator+1:]
	}
	return qualified
}

func isXMLSpace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\r' || character == '\n'
}

// RedactText removes XML token secrets, URI userinfo/query data, and HTTP
// authorization header values from arbitrary diagnostics.
func RedactText(value string) string {
	return redactPlainText(string(RedactXML([]byte(value))))
}

func redactPlainText(value string) string {
	// These loose forms cover parser/error text that contains an assignment but
	// not a complete XML tag. Opposite quote characters are valid content and do
	// not terminate the selected quote style.
	value = doubleQuotedLooseSecret.ReplaceAllString(value, `${1}="`+RedactedValue+`"`)
	value = singleQuotedLooseSecret.ReplaceAllString(value, `${1}='`+RedactedValue+`'`)
	value = unquotedLooseSecret.ReplaceAllString(value, `${1}="`+RedactedValue+`"`)
	redacted := userinfoPattern.ReplaceAllString(value, `${1}`+RedactedValue+`@`)
	redacted = urlQueryPattern.ReplaceAllString(redacted, `${1}?`+RedactedValue)
	return authorizationLinePattern.ReplaceAllStringFunc(redacted, func(line string) string {
		separator := strings.IndexByte(line, ':')
		if separator < 0 {
			return RedactedValue
		}
		return line[:separator+1] + " " + RedactedValue
	})
}

// sanitizeDiagnostic bounds a diagnostic message, strips credential-bearing
// fragments, and guarantees valid UTF-8.
func sanitizeDiagnostic(message string) string {
	return string(clipValidUTF8([]byte(RedactText(message)), maxDiagnosticBytes))
}

func clipValidUTF8(value []byte, limit int64) []byte {
	valid := []byte(strings.ToValidUTF8(string(value), ""))
	if limit < 0 || int64(len(valid)) <= limit {
		return valid
	}
	if limit == 0 {
		return nil
	}
	clipped := valid[:int(limit)]
	for len(clipped) > 0 && !utf8.Valid(clipped) {
		clipped = clipped[:len(clipped)-1]
	}
	return clipped
}

// RedactURL removes URI userinfo and query data while retaining endpoint
// context.
func RedactURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return redactPlainText(value)
	}
	if parsed.User != nil {
		parsed.User = url.User(RedactedValue)
	}
	if parsed.RawQuery != "" {
		parsed.RawQuery = RedactedValue
	}
	return parsed.String()
}

// RedactHeaders returns an independent copy with authorization values removed.
func RedactHeaders(headers http.Header) http.Header {
	redacted := headers.Clone()
	for key := range redacted {
		if strings.EqualFold(key, "Authorization") || strings.EqualFold(key, "Proxy-Authorization") {
			redacted[key] = []string{RedactedValue}
		}
	}
	return redacted
}
