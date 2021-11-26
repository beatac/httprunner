package har2case

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pkg/errors"

	"github.com/httprunner/hrp"
	"github.com/httprunner/hrp/internal/ga"
)

var log = hrp.GetLogger()

const (
	suffixJSON = ".json"
	suffixYAML = ".yaml"
)

func NewHAR(path string) *HAR {
	return &HAR{
		path: path,
	}
}

type HAR struct {
	path       string
	filterStr  string
	excludeStr string
	outputDir  string
}

func (h *HAR) SetOutputDir(dir string) {
	h.outputDir = dir
}

func (h *HAR) GenJSON() (jsonPath string, err error) {
	event := ga.EventTracking{
		Category: "har2case",
		Action:   "hrp har2case --to-json",
	}
	// report start event
	go ga.SendEvent(event)
	// report running timing event
	defer ga.SendEvent(event.StartTiming("execution"))

	tCase, err := h.makeTestCase()
	if err != nil {
		return "", err
	}
	jsonPath = h.genOutputPath(suffixJSON)
	err = tCase.Dump2JSON(jsonPath)
	return
}

func (h *HAR) GenYAML() (yamlPath string, err error) {
	event := ga.EventTracking{
		Category: "har2case",
		Action:   "hrp har2case --to-yaml",
	}
	// report start event
	go ga.SendEvent(event)
	// report running timing event
	defer ga.SendEvent(event.StartTiming("execution"))

	tCase, err := h.makeTestCase()
	if err != nil {
		return "", err
	}
	yamlPath = h.genOutputPath(suffixYAML)
	err = tCase.Dump2YAML(yamlPath)
	return
}

func (h *HAR) makeTestCase() (*hrp.TCase, error) {
	teststeps, err := h.prepareTestSteps()
	if err != nil {
		return nil, err
	}

	tCase := &hrp.TCase{
		Config:    *h.prepareConfig(),
		TestSteps: teststeps,
	}
	return tCase, nil
}

func (h *HAR) load() (*Har, error) {
	fp, err := os.Open(h.path)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}

	data, err := ioutil.ReadAll(fp)
	fp.Close()
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	har := &Har{}
	err = json.Unmarshal(data, har)
	if err != nil {
		return nil, fmt.Errorf("json.Unmarshal error: %w", err)
	}

	return har, nil
}

func (h *HAR) prepareConfig() *hrp.TConfig {
	return &hrp.TConfig{
		Name:      "testcase description",
		Variables: make(map[string]interface{}),
		Verify:    false,
	}
}

func (h *HAR) prepareTestSteps() ([]*hrp.TStep, error) {
	har, err := h.load()
	if err != nil {
		return nil, err
	}

	var steps []*hrp.TStep
	for _, entry := range har.Log.Entries {
		step, err := h.prepareTestStep(&entry)
		if err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}

	return steps, nil
}

func (h *HAR) prepareTestStep(entry *Entry) (*hrp.TStep, error) {
	log.Info().
		Str("method", entry.Request.Method).
		Str("url", entry.Request.URL).
		Msg("convert teststep")

	tStep := &TStep{
		TStep: hrp.TStep{
			Request:    &hrp.TRequest{},
			Validators: make([]hrp.TValidator, 0),
		},
	}
	if err := tStep.makeRequestMethod(entry); err != nil {
		return nil, err
	}
	if err := tStep.makeRequestURL(entry); err != nil {
		return nil, err
	}
	if err := tStep.makeRequestParams(entry); err != nil {
		return nil, err
	}
	if err := tStep.makeRequestCookies(entry); err != nil {
		return nil, err
	}
	if err := tStep.makeRequestHeaders(entry); err != nil {
		return nil, err
	}
	if err := tStep.makeRequestBody(entry); err != nil {
		return nil, err
	}
	if err := tStep.makeValidate(entry); err != nil {
		return nil, err
	}
	return &tStep.TStep, nil
}

type TStep struct {
	hrp.TStep
}

func (s *TStep) makeRequestMethod(entry *Entry) error {
	s.Request.Method = hrp.EnumHTTPMethod(entry.Request.Method)
	return nil
}

func (s *TStep) makeRequestURL(entry *Entry) error {

	u, err := url.Parse(entry.Request.URL)
	if err != nil {
		log.Error().Err(err).Msg("make request url failed")
		return err
	}
	s.Request.URL = fmt.Sprintf("%s://%s", u.Scheme, u.Hostname()+u.Path)
	return nil
}

func (s *TStep) makeRequestParams(entry *Entry) error {
	s.Request.Params = make(map[string]interface{})
	for _, param := range entry.Request.QueryString {
		s.Request.Params[param.Name] = param.Value
	}
	return nil
}

func (s *TStep) makeRequestCookies(entry *Entry) error {
	s.Request.Cookies = make(map[string]string)
	for _, cookie := range entry.Request.Cookies {
		s.Request.Cookies[cookie.Name] = cookie.Value
	}
	return nil
}

func (s *TStep) makeRequestHeaders(entry *Entry) error {
	s.Request.Headers = make(map[string]string)
	for _, header := range entry.Request.Headers {
		if strings.EqualFold(header.Name, "cookie") {
			continue
		}
		s.Request.Headers[header.Name] = header.Value
	}
	return nil
}

func (s *TStep) makeRequestBody(entry *Entry) error {
	mimeType := entry.Request.PostData.MimeType
	if mimeType == "" {
		// GET/HEAD/DELETE without body
		return nil
	}

	// POST/PUT with body
	if strings.HasPrefix(mimeType, "application/json") {
		// post json
		var body interface{}
		err := json.Unmarshal([]byte(entry.Request.PostData.Text), &body)
		if err != nil {
			log.Error().Err(err).Msg("make request body failed")
			return err
		}
		s.Request.Body = body
	} else if strings.HasPrefix(mimeType, "application/x-www-form-urlencoded") {
		// post form
		var paramsList []string
		for _, param := range entry.Request.PostData.Params {
			paramsList = append(paramsList, fmt.Sprintf("%s=%s", param.Name, param.Value))
		}
		s.Request.Body = strings.Join(paramsList, "&")
	} else if strings.HasPrefix(mimeType, "text/plain") {
		// post raw data
		s.Request.Body = entry.Request.PostData.Text
	} else {
		// TODO
		log.Error().Msgf("makeRequestBody: Not implemented for mimeType %s", mimeType)
	}
	return nil
}

func (s *TStep) makeValidate(entry *Entry) error {
	// make validator for response status code
	s.Validators = append(s.Validators, hrp.TValidator{
		Check:   "status_code",
		Assert:  "equals",
		Expect:  entry.Response.Status,
		Message: "assert response status code",
	})

	// make validators for response headers
	for _, header := range entry.Response.Headers {
		// assert Content-Type
		if strings.EqualFold(header.Name, "Content-Type") {
			s.Validators = append(s.Validators, hrp.TValidator{
				Check:   "headers.Content-Type",
				Assert:  "equals",
				Expect:  header.Value,
				Message: "assert response header Content-Type",
			})
		}
	}

	// make validators for response body
	respBody := entry.Response.Content
	if respBody.Text == "" {
		// response body is empty
		return nil
	}
	if strings.HasPrefix(respBody.MimeType, "application/json") {
		// response body is json
		if respBody.Encoding == "base64" {
			// decode base64 text
			data, err := base64.StdEncoding.DecodeString(respBody.Text)
			if err != nil {
				return errors.Wrap(err, "decode base64 error")
			}

			// convert to json
			var body interface{}
			if err = json.Unmarshal(data, &body); err != nil {
				return errors.Wrap(err, "json.Unmarshal body error")
			}
			jsonBody, ok := body.(map[string]interface{})
			if !ok {
				return fmt.Errorf("response body is not json, not matched with MimeType")
			}

			// response body is json
			keys := make([]string, 0, len(jsonBody))
			for k := range jsonBody {
				keys = append(keys, k)
			}
			// sort map keys to keep validators in stable order
			sort.Strings(keys)
			for _, key := range keys {
				value := jsonBody[key]
				switch v := value.(type) {
				case map[string]interface{}:
					continue
				case []interface{}:
					continue
				default:
					s.Validators = append(s.Validators, hrp.TValidator{
						Check:   fmt.Sprintf("body.%s", key),
						Assert:  "equals",
						Expect:  v,
						Message: fmt.Sprintf("assert response body %s", key),
					})
				}
			}
		}
	}

	return nil
}

func (h *HAR) genOutputPath(suffix string) string {
	file := getFilenameWithoutExtension(h.path) + suffix
	if h.outputDir != "" {
		return filepath.Join(h.outputDir, file)
	} else {
		return filepath.Join(filepath.Dir(h.path), file)
	}
}

func getFilenameWithoutExtension(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return base[0 : len(base)-len(ext)]
}
