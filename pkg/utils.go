package pkg

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"strconv"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

func GenTimestamp() uint32 {
	s := time.Now().Format("0102150405")
	i, _ := strconv.Atoi(s)
	return uint32(i)
}

func GenTimestampYYStr(t int64) string {
	return time.Unix(t, 0).Format("0601021504")
}

func GenNowTimeYYYYStr() string {
	s := time.Now().Format("20060102150405")
	return s
}

func GenNowTimeYYStr() string {
	return time.Unix(time.Now().Unix(), 0).Format("0601021504")
}

func GenMsgID(sequenceNum uint32) string {
	now := time.Now()
	month, _ := strconv.ParseInt(fmt.Sprintf("%d", now.Month()), 10, 32)
	day := now.Day()
	hour := now.Hour()
	min := now.Minute()
	sec := now.Second()
	binaryStr := fmt.Sprintf("%04b%05b%05b%06b%06b%032b", month, day, hour, min, sec, sequenceNum)
	msgId, err := strconv.ParseUint(binaryStr, 2, 64)
	if err != nil {
		return ""
	}
	return strconv.Itoa(int(msgId))
}

func Utf8ToUcs2(in string) (string, error) {
	if !utf8.ValidString(in) {
		return "", errors.New("invalid utf8 runes")
	}

	r := bytes.NewReader([]byte(in))
	t := transform.NewReader(r, unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM).NewEncoder()) //UTF-16 bigendian, no-bom
	out, err := ioutil.ReadAll(t)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func Ucs2ToUtf8(in string) (string, error) {
	r := bytes.NewReader([]byte(in))
	t := transform.NewReader(r, unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM).NewDecoder()) //UTF-16 bigendian, no-bom
	out, err := ioutil.ReadAll(t)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func Utf8ToGB18030(in string) (string, error) {
	if !utf8.ValidString(in) {
		return "", errors.New("invalid utf8 runes")
	}

	r := bytes.NewReader([]byte(in))
	t := transform.NewReader(r, simplifiedchinese.GB18030.NewEncoder())
	out, err := ioutil.ReadAll(t)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func GB18030ToUtf8(in string) (string, error) {
	r := bytes.NewReader([]byte(in))
	t := transform.NewReader(r, simplifiedchinese.GB18030.NewDecoder())
	out, err := ioutil.ReadAll(t)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func GetUtf8Content(msgFmt uint8, msgContent string) (string, error) {
	switch msgFmt {
	case ASCII:
		return Decode7Bit([]byte(msgContent))
	case BINARY:
	case UCS2:
		return Ucs2ToUtf8(msgContent)
	case GB18030:
		return GB18030ToUtf8(msgContent)
	default:
	}

	return "", errors.New("invalid msg fmt")
}

var TpUdhiSeq byte = 0x00

func SplitLongSms(msgFmt uint8, content string, contentBytes []byte) [][]byte {
	smsLength := 140
	smsHeaderLength := 6
	if msgFmt == ASCII {
		// 7-Bit 160 个字符
		smsLength = 160
	}
	smsBodyLen := smsLength - smsHeaderLength
	var chunks [][]byte
	num := 1
	contentLen := len(content)
	if (contentLen) > smsLength {
		num = int(math.Ceil(float64(contentLen) / float64(smsBodyLen)))
	}
	if num == 1 {
		chunks = append(chunks, contentBytes)
		return chunks
	}
	tpUdhiHeader := []byte{0x05, 0x00, 0x03, TpUdhiSeq, byte(num)}
	TpUdhiSeq++

	for i := 0; i < num; i++ {
		chunk := tpUdhiHeader
		chunk = append(chunk, byte(i+1))
		offset := i * smsBodyLen
		max := offset + smsBodyLen
		if max > contentLen {
			max = contentLen
		}

		chunk = append(chunk, contentBytes[offset:max]...)
		chunks = append(chunks, chunk)
	}
	return chunks
}

func GetMsgPkgs(pkg *SmppSubmitReqPkt) ([]*SmppSubmitReqPkt, error) {
	packets := make([]*SmppSubmitReqPkt, 0)
	contentByte := make([]byte, 0)
	var (
		err     error
		content string
	)

	switch pkg.DataCoding {
	case UCS2:
		content, err = Utf8ToUcs2(pkg.ShortMessage)
		if err != nil {
			return packets, err
		} else {
			contentByte = []byte(content)
		}
	case ASCII:
		contentByte, err = Encode7Bit(pkg.ShortMessage)
		if err != nil {
			content, err = Utf8ToUcs2(pkg.ShortMessage)
			if err != nil {
				return packets, err
			} else {
				contentByte = []byte(content)
				pkg.DataCoding = UCS2
			}
		}
		content = string(contentByte)
	default:
		return packets, fmt.Errorf("data_coding: met an unexpected data_coding [%d]", pkg.DataCoding)
	}

	chunks := SplitLongSms(pkg.DataCoding, content, contentByte)
	var esmClass uint8
	if len(chunks) > 1 {
		esmClass = SM_UDH_GSM
	}

	for _, chunk := range chunks {
		p := &SmppSubmitReqPkt{
			ServiceType:        pkg.ServiceType,
			SourceAddrTON:      pkg.SourceAddrTON,
			SourceAddrNPI:      pkg.SourceAddrNPI,
			SourceAddr:         pkg.SourceAddr,
			DestAddrTON:        pkg.DestAddrTON,
			DestAddrNPI:        pkg.DestAddrNPI,
			DestinationAddr:    pkg.DestinationAddr, // phone
			EsmClass:           esmClass,
			PriorityFlag:       NORMAL_PRIORITY,
			RegisteredDelivery: NEED_REPORT,
			DataCoding:         pkg.DataCoding,
			SmLength:           uint8(len(chunk)),
			ShortMessage:       string(chunk),
		}
		packets = append(packets, p)
	}
	return packets, nil
}

/*
GSM 7-bit default alphabet and extension table
Source: https://en.wikipedia.org/wiki/GSM_03.38#GSM_7-bit_default_alphabet_and_extension_table_of_3GPP_TS_23.038_/_GSM_03.38
*/
const escapeSequence = 0x1B

var forwardLookup = map[rune]byte{
	'@': 0x00, '£': 0x01, '$': 0x02, '¥': 0x03, 'è': 0x04, 'é': 0x05, 'ù': 0x06, 'ì': 0x07,
	'ò': 0x08, 'Ç': 0x09, '\n': 0x0a, 'Ø': 0x0b, 'ø': 0x0c, '\r': 0x0d, 'Å': 0x0e, 'å': 0x0f,
	'Δ': 0x10, '_': 0x11, 'Φ': 0x12, 'Γ': 0x13, 'Λ': 0x14, 'Ω': 0x15, 'Π': 0x16, 'Ψ': 0x17,
	'Σ': 0x18, 'Θ': 0x19, 'Ξ': 0x1a /* 0x1B */, 'Æ': 0x1c, 'æ': 0x1d, 'ß': 0x1e, 'É': 0x1f,
	' ': 0x20, '!': 0x21, '"': 0x22, '#': 0x23, '¤': 0x24, '%': 0x25, '&': 0x26, '\'': 0x27,
	'(': 0x28, ')': 0x29, '*': 0x2a, '+': 0x2b, ',': 0x2c, '-': 0x2d, '.': 0x2e, '/': 0x2f,
	'0': 0x30, '1': 0x31, '2': 0x32, '3': 0x33, '4': 0x34, '5': 0x35, '6': 0x36, '7': 0x37,
	'8': 0x38, '9': 0x39, ':': 0x3a, ';': 0x3b, '<': 0x3c, '=': 0x3d, '>': 0x3e, '?': 0x3f,
	'¡': 0x40, 'A': 0x41, 'B': 0x42, 'C': 0x43, 'D': 0x44, 'E': 0x45, 'F': 0x46, 'G': 0x47,
	'H': 0x48, 'I': 0x49, 'J': 0x4a, 'K': 0x4b, 'L': 0x4c, 'M': 0x4d, 'N': 0x4e, 'O': 0x4f,
	'P': 0x50, 'Q': 0x51, 'R': 0x52, 'S': 0x53, 'T': 0x54, 'U': 0x55, 'V': 0x56, 'W': 0x57,
	'X': 0x58, 'Y': 0x59, 'Z': 0x5a, 'Ä': 0x5b, 'Ö': 0x5c, 'Ñ': 0x5d, 'Ü': 0x5e, '§': 0x5f,
	'¿': 0x60, 'a': 0x61, 'b': 0x62, 'c': 0x63, 'd': 0x64, 'e': 0x65, 'f': 0x66, 'g': 0x67,
	'h': 0x68, 'i': 0x69, 'j': 0x6a, 'k': 0x6b, 'l': 0x6c, 'm': 0x6d, 'n': 0x6e, 'o': 0x6f,
	'p': 0x70, 'q': 0x71, 'r': 0x72, 's': 0x73, 't': 0x74, 'u': 0x75, 'v': 0x76, 'w': 0x77,
	'x': 0x78, 'y': 0x79, 'z': 0x7a, 'ä': 0x7b, 'ö': 0x7c, 'ñ': 0x7d, 'ü': 0x7e, 'à': 0x7f,
}
var forwardEscape = map[rune]byte{
	'\f': 0x0A, '^': 0x14, '{': 0x28, '}': 0x29, '\\': 0x2F, '[': 0x3C, '~': 0x3D, ']': 0x3E, '|': 0x40, '€': 0x65,
}
var reverseLookup = map[byte]rune{
	0x00: '@', 0x01: '£', 0x02: '$', 0x03: '¥', 0x04: 'è', 0x05: 'é', 0x06: 'ù', 0x07: 'ì',
	0x08: 'ò', 0x09: 'Ç', 0x0a: '\n', 0x0b: 'Ø', 0x0c: 'ø', 0x0d: '\r', 0x0e: 'Å', 0x0f: 'å',
	0x10: 'Δ', 0x11: '_', 0x12: 'Φ', 0x13: 'Γ', 0x14: 'Λ', 0x15: 'Ω', 0x16: 'Π', 0x17: 'Ψ',
	0x18: 'Σ', 0x19: 'Θ', 0x1a: 'Ξ' /* 0x1B */, 0x1c: 'Æ', 0x1d: 'æ', 0x1e: 'ß', 0x1f: 'É',
	0x20: ' ', 0x21: '!', 0x22: '"', 0x23: '#', 0x24: '¤', 0x25: '%', 0x26: '&', 0x27: '\'',
	0x28: '(', 0x29: ')', 0x2a: '*', 0x2b: '+', 0x2c: ',', 0x2d: '-', 0x2e: '.', 0x2f: '/',
	0x30: '0', 0x31: '1', 0x32: '2', 0x33: '3', 0x34: '4', 0x35: '5', 0x36: '6', 0x37: '7',
	0x38: '8', 0x39: '9', 0x3a: ':', 0x3b: ';', 0x3c: '<', 0x3d: '=', 0x3e: '>', 0x3f: '?',
	0x40: '¡', 0x41: 'A', 0x42: 'B', 0x43: 'C', 0x44: 'D', 0x45: 'E', 0x46: 'F', 0x47: 'G',
	0x48: 'H', 0x49: 'I', 0x4a: 'J', 0x4b: 'K', 0x4c: 'L', 0x4d: 'M', 0x4e: 'N', 0x4f: 'O',
	0x50: 'P', 0x51: 'Q', 0x52: 'R', 0x53: 'S', 0x54: 'T', 0x55: 'U', 0x56: 'V', 0x57: 'W',
	0x58: 'X', 0x59: 'Y', 0x5a: 'Z', 0x5b: 'Ä', 0x5c: 'Ö', 0x5d: 'Ñ', 0x5e: 'Ü', 0x5f: '§',
	0x60: '¿', 0x61: 'a', 0x62: 'b', 0x63: 'c', 0x64: 'd', 0x65: 'e', 0x66: 'f', 0x67: 'g',
	0x68: 'h', 0x69: 'i', 0x6a: 'j', 0x6b: 'k', 0x6c: 'l', 0x6d: 'm', 0x6e: 'n', 0x6f: 'o',
	0x70: 'p', 0x71: 'q', 0x72: 'r', 0x73: 's', 0x74: 't', 0x75: 'u', 0x76: 'v', 0x77: 'w',
	0x78: 'x', 0x79: 'y', 0x7a: 'z', 0x7b: 'ä', 0x7c: 'ö', 0x7d: 'ñ', 0x7e: 'ü', 0x7f: 'à',
}
var reverseEscape = map[byte]rune{
	0x0A: '\f', 0x14: '^', 0x28: '{', 0x29: '}', 0x2F: '\\', 0x3C: '[', 0x3D: '~', 0x3E: ']', 0x40: '|', 0x65: '€',
}

// ErrInvalidCharacter means a given character can not be represented in GSM 7-bit encoding.
//
// This can only happen during encoding.
var ErrInvalidCharacter = errors.New("invalid gsm7 character")

// ErrInvalidByte means that a given byte is outside of the GSM 7-bit encoding range.
//
// This can only happen during decoding.
var ErrInvalidByte = errors.New("invalid gsm7 byte")

// Returns the characters, in the given text, that can not be represented in GSM 7-bit encoding.
func ValidateGSM7String(text string) []rune {
	invalidChars := make([]rune, 0, 4)
	for _, r := range text {
		if _, ok := forwardLookup[r]; !ok {
			if _, ok := forwardEscape[r]; !ok {
				invalidChars = append(invalidChars, r)
			}
		}
	}
	return invalidChars
}

// Returns the bytes, in the given buffer, that are outside of the GSM 7-bit encoding range.
func ValidateGSM7Buffer(buffer []byte) []byte {
	invalidBytes := make([]byte, 0, 4)
	count := 0
	for count < len(buffer) {
		b := buffer[count]
		if b == escapeSequence {
			count++
			if count >= len(buffer) {
				invalidBytes = append(invalidBytes, b)
				break
			}
			e := buffer[count]
			if _, ok := reverseEscape[e]; !ok {
				invalidBytes = append(invalidBytes, b, e)
			}
		} else if _, ok := reverseLookup[b]; !ok {
			invalidBytes = append(invalidBytes, b)
		}
		count++
	}
	return invalidBytes
}

func NewGSM7Decoder() *encoding.Decoder {
	return &encoding.Decoder{Transformer: &gsm7Decoder{}}
}

func NewGSM7Encoder() *encoding.Encoder {
	return &encoding.Encoder{Transformer: &gsm7Encoder{}}
}

type gsm7Decoder struct{}

func (g *gsm7Decoder) Reset() {
	/* not needed */
}

func (g *gsm7Decoder) Transform(dst, src []byte, atEOF bool) (nDst, nSrc int, err error) {
	if len(src) == 0 {
		return 0, 0, nil
	}

	septets := src
	nSeptet := 0
	builder := bytes.NewBufferString("")
	for nSeptet < len(septets) {
		b := septets[nSeptet]
		if b == escapeSequence {
			nSeptet++
			if nSeptet >= len(septets) {
				return 0, 0, ErrInvalidByte
			}
			e := septets[nSeptet]
			if r, ok := reverseEscape[e]; ok {
				builder.WriteRune(r)
			} else {
				return 0, 0, ErrInvalidByte
			}
		} else if r, ok := reverseLookup[b]; ok {
			builder.WriteRune(r)
		} else {
			return 0, 0, ErrInvalidByte
		}
		nSeptet++
	}
	text := builder.Bytes()
	nDst = len(text)

	if len(dst) < nDst {
		return 0, 0, transform.ErrShortDst
	}

	for x, b := range text {
		dst[x] = b
	}
	return nDst, nSrc, err
}

type gsm7Encoder struct{}

func (g *gsm7Encoder) Reset() {
	/* no needed */
}

func (g *gsm7Encoder) Transform(dst, src []byte, atEOF bool) (nDst, nSrc int, err error) {
	if len(src) == 0 {
		return 0, 0, nil
	}

	text := string(src) // work with []rune (a.k.a string) instead of []byte
	septets := make([]byte, 0, len(text))
	for _, r := range text {
		if v, ok := forwardLookup[r]; ok {
			septets = append(septets, v)
		} else if v, ok := forwardEscape[r]; ok {
			septets = append(septets, escapeSequence, v)
		} else {
			return 0, 0, ErrInvalidCharacter
		}
		nSrc++
	}

	nDst = len(septets)
	if len(dst) < nDst {
		return 0, 0, transform.ErrShortDst
	}

	for x, v := range septets {
		dst[x] = v
	}
	return nDst, nSrc, nil
}

func Encode7Bit(s string) ([]byte, error) {
	encoder := NewGSM7Encoder()
	result, _, err := transform.Bytes(encoder, []byte(s))
	return result, err
}

func Decode7Bit(b []byte) (string, error) {
	decoder := NewGSM7Decoder()
	result, _, err := transform.Bytes(decoder, b)
	return string(result), err
}
