package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/comunifi/relay/pkg/relay"
)

type Message struct {
	Content string `json:"content"`
}

type Messager struct {
	BaseURL    string
	ServerName string

	notify bool
}

func NewMessager(baseURL, serverName string, notify bool) relay.WebhookMessager {
	return &Messager{
		BaseURL:    baseURL,
		ServerName: serverName,
		notify:     notify,
	}
}

func (b *Messager) Notify(ctx context.Context, message string) error {
	if !b.notify {
		return nil
	}

	data, err := json.Marshal(Message{Content: fmt.Sprintf("[%s] %s", b.ServerName, message)})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.BaseURL, bytes.NewReader(data))
	if err != nil {
		return err
	}

	req.Header.Add("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("error sending message")
	}

	return nil
}

func (b *Messager) NotifyWarning(ctx context.Context, errorMessage error) error {
	if !b.notify {
		return nil
	}

	data, err := json.Marshal(Message{Content: fmt.Sprintf("[%s] warning: %s", b.ServerName, errorMessage.Error())})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.BaseURL, bytes.NewReader(data))
	if err != nil {
		return err
	}

	req.Header.Add("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("error sending message")
	}

	return nil
}

func (b *Messager) NotifyError(ctx context.Context, errorMessage error) error {
	if !b.notify {
		return nil
	}

	data, err := json.Marshal(Message{Content: fmt.Sprintf("[%s] error: %s", b.ServerName, errorMessage.Error())})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.BaseURL, bytes.NewReader(data))
	if err != nil {
		return err
	}

	req.Header.Add("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("error sending message")
	}

	return nil
}
