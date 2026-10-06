package whatsapp

import "context"

// ListSection is the exported type used when calling SendList.
type ListSection struct {
	Title string
	Rows  [][3]string // [id, title, description]
}

// Messenger is the interface implemented by the Meta WhatsApp Cloud API client.
type Messenger interface {
	SendText(ctx context.Context, to, text string) error
	SendImage(ctx context.Context, to, imageURL, caption string) error
	SendButtonsWithImageHeader(ctx context.Context, to, bodyText, imageURL string, buttons [][2]string) error
	SendList(ctx context.Context, to, bodyText, buttonLabel string, sections []ListSection) error
}
