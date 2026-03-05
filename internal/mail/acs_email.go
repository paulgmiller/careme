package mail

import (
	"bytes"
	"careme/internal/config"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const acsEmailAPIVersion = "2023-03-31"

type acsEmailClient struct {
	endpoint      *url.URL
	senderAddress string
	accessKey     []byte
	httpClient    *http.Client
	now           func() time.Time
	apiVersion    string
}

type acsEmailSendRequest struct {
	SenderAddress string                    `json:"senderAddress"`
	Recipients    acsEmailRequestRecipients `json:"recipients"`
	Content       acsEmailRequestContent    `json:"content"`
}

type acsEmailRequestRecipients struct {
	To []acsEmailAddress `json:"to"`
}

type acsEmailAddress struct {
	Address     string `json:"address"`
	DisplayName string `json:"displayName,omitempty"`
}

type acsEmailRequestContent struct {
	Subject   string `json:"subject"`
	PlainText string `json:"plainText,omitempty"`
	HTML      string `json:"html,omitempty"`
}

func newACSEmailClientFromConfig(cfg *config.Config) (emailClient, string, error) {
	if cfg == nil {
		return nil, "", fmt.Errorf("config is required")
	}
	if cfg.ACS.EmailEndpoint == "" {
		return nil, "", fmt.Errorf("ACS_EMAIL_ENDPOINT environment variable is not set")
	}
	if cfg.ACS.EmailAccessKey == "" {
		return nil, "", fmt.Errorf("ACS_EMAIL_ACCESS_KEY environment variable is not set")
	}
	if cfg.ACS.EmailSender == "" {
		return nil, "", fmt.Errorf("ACS_EMAIL_SENDER environment variable is not set")
	}

	client, err := newACSEmailClient(cfg.ACS.EmailEndpoint, cfg.ACS.EmailSender, cfg.ACS.EmailAccessKey)
	if err != nil {
		return nil, "", err
	}
	return client, cfg.ACS.EmailSender, nil
}

func newACSEmailClient(endpoint, senderAddress, accessKey string) (*acsEmailClient, error) {
	parsedEndpoint, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return nil, fmt.Errorf("parse ACS email endpoint: %w", err)
	}
	if parsedEndpoint.Scheme == "" || parsedEndpoint.Host == "" {
		return nil, fmt.Errorf("ACS email endpoint must include scheme and host")
	}

	decodedAccessKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(accessKey))
	if err != nil {
		return nil, fmt.Errorf("decode ACS email access key: %w", err)
	}

	return &acsEmailClient{
		endpoint:      parsedEndpoint,
		senderAddress: strings.TrimSpace(senderAddress),
		accessKey:     decodedAccessKey,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
		now:           time.Now,
		apiVersion:    acsEmailAPIVersion,
	}, nil
}

func (c *acsEmailClient) Send(ctx context.Context, message EmailMessage) (*SendResult, error) {
	if len(message.To) == 0 {
		return nil, fmt.Errorf("at least one recipient is required")
	}

	senderAddress := strings.TrimSpace(message.FromAddress)
	if senderAddress == "" {
		senderAddress = c.senderAddress
	}
	if senderAddress == "" {
		return nil, fmt.Errorf("sender address is required")
	}

	recipients := make([]acsEmailAddress, 0, len(message.To))
	for _, recipient := range message.To {
		trimmed := strings.TrimSpace(recipient)
		if trimmed == "" {
			continue
		}
		recipients = append(recipients, acsEmailAddress{
			Address:     trimmed,
			DisplayName: trimmed,
		})
	}
	if len(recipients) == 0 {
		return nil, fmt.Errorf("at least one non-empty recipient is required")
	}

	requestBody, err := json.Marshal(acsEmailSendRequest{
		SenderAddress: senderAddress,
		Recipients: acsEmailRequestRecipients{
			To: recipients,
		},
		Content: acsEmailRequestContent{
			Subject:   message.Subject,
			PlainText: message.PlainTextContent,
			HTML:      message.HTMLContent,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("encode ACS email request: %w", err)
	}

	sendURL := *c.endpoint
	sendURL.Path = strings.TrimRight(sendURL.Path, "/") + "/emails:send"
	query := sendURL.Query()
	query.Set("api-version", c.apiVersion)
	sendURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sendURL.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("build ACS email request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if err := c.signRequest(req, requestBody); err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send ACS email request: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read ACS email response: %w", err)
	}

	messageID := parseACSEmailMessageID(resp, responseBody)
	return &SendResult{
		StatusCode: resp.StatusCode,
		Body:       string(responseBody),
		Headers:    resp.Header.Clone(),
		MessageID:  messageID,
	}, nil
}

func (c *acsEmailClient) signRequest(req *http.Request, payload []byte) error {
	payloadHashBytes := sha256.Sum256(payload)
	contentHash := base64.StdEncoding.EncodeToString(payloadHashBytes[:])

	requestDate := c.now().UTC().Format(http.TimeFormat)
	signedHeaders := "x-ms-date;host;x-ms-content-sha256"
	stringToSign := req.Method + "\n" + req.URL.RequestURI() + "\n" + requestDate + ";" + req.URL.Host + ";" + contentHash

	h := hmac.New(sha256.New, c.accessKey)
	if _, err := h.Write([]byte(stringToSign)); err != nil {
		return fmt.Errorf("sign ACS email request: %w", err)
	}
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	req.Header.Set("x-ms-date", requestDate)
	req.Header.Set("x-ms-content-sha256", contentHash)
	req.Header.Set("Authorization", "HMAC-SHA256 SignedHeaders="+signedHeaders+"&Signature="+signature)
	return nil
}

func parseACSEmailMessageID(resp *http.Response, responseBody []byte) string {
	if operationLocation := resp.Header.Get("Operation-Location"); operationLocation != "" {
		return operationLocation
	}
	if operationID := resp.Header.Get("x-ms-operation-id"); operationID != "" {
		return operationID
	}
	if requestID := resp.Header.Get("x-ms-request-id"); requestID != "" {
		return requestID
	}

	var response struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(responseBody, &response); err == nil {
		return response.ID
	}
	return ""
}
