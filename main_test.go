package gophermail

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	. "github.com/onsi/gomega"
	"io"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

// registerFailHandler registers a gomega fail handler that calls t.Fatal
// gomega.RegisterTestingT calls t.Error, which does not stop the test
func registerFailHandler(t *testing.T) {
	RegisterFailHandler(func(message string, callerSkip ...int) {
		t.Fatalf("\n%s", message)
	})
}

// expectNoError fails the test if err is not nil
func expectNoError(err error) {
	Expect(err).To(BeNil(), fmt.Sprintf("%v", err))
}

// getContentType gets the content type in a header, and parses it
func getContentType(header textproto.MIMEHeader) (string, map[string]string) {
	contentType, ok := header["Content-Type"]
	Expect(ok).To(BeTrue(), "Content-Type header not found")
	Expect(contentType).NotTo(BeEmpty(), "Content-Type header is empty")
	Expect(contentType).To(HaveLen(1), "More than one Content-Type header found")

	mediaType, params, err := mime.ParseMediaType(contentType[0])
	expectNoError(err)

	return mediaType, params
}

func matchBase64(r io.Reader, expected string, msg string) {
	base64contents, err := ioutil.ReadAll(r)
	expectNoError(err)

	contents, err := base64.StdEncoding.DecodeString(string(base64contents))
	expectNoError(err)
	Expect(string(contents)).To(Equal(expected), msg)
}

// testMail is the main testing function
func testMail(t *testing.T, plain, html, attachment bool) {
	registerFailHandler(t)

	// NOTE: QP decoding cuts off trailing whitespace
	plainBody := "My Plain Text Body áűőú\n Lorem ipsum dolor sit amet, consectetur adipiscing elit.\n Nunc et purus massa. Maecenas sed ex iaculis, feugiat elit ullamcorper, eleifend elit. Aliquam ultricies libero vitae interdum maximus. Nullam placerat purus dolor, a tempor magna efficitur in. Integer mattis, lacus tempus mattis rutrum, tellus velit ultricies nisl, a elementum dolor nisi sed diam."
	htmlBody := "<p>My <b>HTML</b> Body</p>\n<p> Lorem ipsum dolor sit amet, consectetur adipiscing elit. Nunc et purus massa.</p>"
	filename := "test.txt"
	fileContents := "Lorem ipsum dolor sit amet, consectetur adipiscing elit. Nunc et purus massa. Aenean sed enim turpis. Maecenas sed ex iaculis, feugiat elit ullamcorper, eleifend elit. Aliquam ultricies libero vitae interdum maximus. Nullam placerat purus dolor, a tempor magna efficitur in. Integer mattis, lacus tempus mattis rutrum, tellus velit ultricies nisl, a elementum dolor nisi sed diam. Nunc cursus arcu quis sapien dapibus suscipit. Aliquam in dolor ut enim faucibus volutpat vel id ipsum. Aenean blandit ipsum eu bibendum fermentum. Donec sagittis nunc dolor, in bibendum lorem pulvinar et. Nam elementum auctor tempor. Nunc et nisl diam. Pellentesque eget suscipit leo. Sed lacus urna, semper nec tellus a, finibus aliquet odio. Nulla in finibus justo, non congue dui."

	m := &Message{}
	m.SetFrom("Doman Sender <sender@domain.com>")
	m.AddTo("First person <to_1@domain.com>")

	m.Subject = "My Subject (abcdefghijklmnop qrstuvwxyz0123456789 abcdefghijklmnopqrstuvwxyz0123456789_567890)"
	if plain {
		m.Body = plainBody
	}
	if html {
		m.HTMLBody = htmlBody
	}

	if attachment {
		m.Attachments = []Attachment{Attachment{
			Name:        filename,
			ContentType: "text/plain",
			Data:        strings.NewReader(fileContents),
		}}
	}

	m.Headers = mail.Header{}
	m.Headers["Date"] = []string{time.Now().UTC().Format(time.RFC822)}

	b, err := m.Bytes()

	expectNoError(err)

	t.Logf("Bytes: \n%s", b)

	byteReader := bytes.NewReader(b)
	bufReader := bufio.NewReader(byteReader)
	headerReader := textproto.NewReader(bufReader)
	header, err := headerReader.ReadMIMEHeader()
	expectNoError(err)

	mediaType, params := getContentType(header)

	if !attachment && !(plain && html) {
		if html {
			Expect(mediaType).To(Equal("text/html"), "Content-Type is not text/html")
		} else {
			Expect(mediaType).To(Equal("text/plain"), "Content-Type is not text/plain")
		}
		return
	}
	if attachment {
		Expect(mediaType).To(Equal("multipart/mixed"), "Content-Type is not multipart/mixed")
	} else {
		Expect(mediaType).To(Equal("multipart/alternative"), "Content-Type is not multipart/alternative")
	}
	Expect(params).To(HaveKey("boundary"), "boundary is missing from Content-Type")
	boundary := params["boundary"]

	plainFound := false
	htmlFound := false
	attachmentFound := false

	var readParts func(string, bool)
	readParts = func(boundary string, toplevel bool) {
		multipartReader := multipart.NewReader(bufReader, boundary)

		for {
			part, err := multipartReader.NextPart()
			if err == io.EOF {
				break
			}
			expectNoError(err)

			t.Logf("part: %v", part)

			mediaType, params := getContentType(part.Header)

			dispositions, ok := part.Header["Content-Disposition"]
			if ok && len(dispositions) == 1 {
				attachmentMediaType, attachmentParams, err := mime.ParseMediaType(dispositions[0])
				expectNoError(err)
				Expect(attachmentMediaType).To(Equal("attachment"), "attachment media type is not \"attachment\"")
				Expect(attachmentParams).To(HaveKey("filename"), "filename missing from attachment")
				Expect(attachmentParams["filename"]).To(Equal(filename), "filename is incorrect")
				Expect(toplevel).To(BeTrue(), "attachment found in multipart/alternative")

				matchBase64(part, fileContents, "attachment does not match")

				attachmentFound = true
			} else {
				switch mediaType {
				case "text/plain":
					rawContents, err := ioutil.ReadAll(part)
					expectNoError(err)

					contents := strings.Replace(string(rawContents), "\r\n", "\n", -1)

					t.Logf("\n\n%#v\n\n%#v\n\n", contents, plainBody)
					expectedBody := plainBody
					if !plain {
						expectedBody = ""
					}
					Expect(contents).To(Equal(expectedBody), "plain text body does not match")

					plainFound = true
				case "text/html":
					matchBase64(part, htmlBody, "html body does not match")

					htmlFound = true
				case "multipart/alternative":
					Expect(params).To(HaveKey("boundary"), "boundary is missing from Content-Type")
					boundary := params["boundary"]
					readParts(boundary, false)
				default:
					t.Logf("unexpected media type: %v", mediaType)
				}
			}
		}
	}

	readParts(boundary, true)
	if plain || !plain && !html {
		Expect(plainFound).To(BeTrue(), "plain text body not found")
	} else {
		Expect(plainFound).NotTo(BeTrue(), "plain text body found")
	}
	if html {
		Expect(htmlFound).To(BeTrue(), "html text body not found")
	} else {
		Expect(htmlFound).NotTo(BeTrue(), "html text body found")
	}
	if attachment {
		Expect(attachmentFound).To(BeTrue(), "attachment not found")
	} else {
		Expect(attachmentFound).NotTo(BeTrue(), "attachment found")
	}
}

func TestPlainBody(t *testing.T) {
	testMail(t, true, false, false)
}

func TestHtmlBody(t *testing.T) {
	testMail(t, false, true, false)
}

func TestAlternativeBody(t *testing.T) {
	testMail(t, true, true, false)
}

func TestPlainAttachment(t *testing.T) {
	testMail(t, true, false, true)
}

func TestHtmlPlainAttachment(t *testing.T) {
	testMail(t, true, true, true)
}

func TestNoBody(t *testing.T) {
	testMail(t, false, false, false)
}

func TestNoBodyAttachment(t *testing.T) {
	testMail(t, false, false, true)
}

func TestAutoDate(t *testing.T) {
	startTime := time.Now()
	m := &Message{}
	m.SetFrom("Doman Sender <sender@domain.com>")
	m.AddTo("First person <to_1@domain.com>")

	m.Body = "Test message"

	b, err := m.Bytes()
	expectNoError(err)

	t.Logf("Bytes: \n%s", b)

	byteReader := bytes.NewReader(b)
	bufReader := bufio.NewReader(byteReader)
	headerReader := textproto.NewReader(bufReader)
	header, err := headerReader.ReadMIMEHeader()
	expectNoError(err)

	dates, ok := header["Date"]
	Expect(ok).To(BeTrue(), "Date header not found")
	Expect(dates).NotTo(BeEmpty(), "Date header is empty")
	Expect(dates).To(HaveLen(1), "More than one Date header found")

	parsedTime, err := time.Parse(time.RFC822, dates[0])

	t.Logf("%v", parsedTime)

	Expect(parsedTime.Before(time.Now().Add(1*time.Minute))).To(BeTrue(), "Time in Date header is too low")
	Expect(parsedTime.After(startTime.Add(-1*time.Minute))).To(BeTrue(), "Time in Date header is too high")
}

func TestManualDate(t *testing.T) {
	msgTime := time.Now().Add(-30 * time.Minute)

	m := &Message{}
	m.SetFrom("Doman Sender <sender@domain.com>")
	m.AddTo("First person <to_1@domain.com>")

	m.Headers = mail.Header{}
	m.Headers["Date"] = []string{msgTime.Format(time.RFC822)}
	m.Body = "Test message"

	b, err := m.Bytes()
	expectNoError(err)

	t.Logf("Bytes: \n%s", b)

	byteReader := bytes.NewReader(b)
	bufReader := bufio.NewReader(byteReader)
	headerReader := textproto.NewReader(bufReader)
	header, err := headerReader.ReadMIMEHeader()
	expectNoError(err)

	dates, ok := header["Date"]
	Expect(ok).To(BeTrue(), "Date header not found")
	Expect(dates).NotTo(BeEmpty(), "Date header is empty")
	Expect(dates).To(HaveLen(1), "More than one Date header found")

	parsedTime, err := time.Parse(time.RFC822, dates[0])

	Expect(parsedTime.Equal(msgTime.Truncate(time.Minute))).To(BeTrue(), "Time in Date header is not what we specified")
}
