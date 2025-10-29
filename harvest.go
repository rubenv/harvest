package harvest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/textproto"
	"net/url"
	"reflect"
	"strconv"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/juju/ratelimit"
	"golang.org/x/sync/errgroup"
)

const serverUrl = "https://api.harvestapp.com/v2"

type Client struct {
	accountID int64
	token     string
	company   *Company

	client *http.Client
	bucket *ratelimit.Bucket
}

type Company struct {
	BaseURI              string `json:"base_uri"`
	FullDomain           string `json:"full_domain"`
	Name                 string `json:"name"`
	IsActive             bool   `json:"is_active"`
	WeekStartDay         string `json:"week_start_day"`
	WantsTimestampTimers bool   `json:"wants_timestamp_timers"`
	TimeFormat           string `json:"time_format"`
	PlanType             string `json:"plan_type"`
	ExpenseFeature       bool   `json:"expense_feature"`
	InvoiceFeature       bool   `json:"invoice_feature"`
	EstimateFeature      bool   `json:"estimate_feature"`
	ApprovalFeature      bool   `json:"approval_feature"`
	Clock                string `json:"clock"`
	DecimalSymbol        string `json:"decimal_symbol"`
	ThousandsSeparator   string `json:"thousands_separator"`
	ColorScheme          string `json:"color_scheme"`
}

type Invoice struct {
	ID             int64     `json:"id,omitempty"`
	ClientID       int64     `json:"client_id,omitempty"`
	ClientKey      string    `json:"client_key,omitempty"`
	Number         string    `json:"number,omitempty"`
	PurchaseOrder  string    `json:"purchase_order,omitempty"`
	State          string    `json:"state,omitempty"`
	SentAt         time.Time `json:"sent_at,omitempty"`
	PaidAt         time.Time `json:"paid_at,omitempty"`
	ClosedAt       time.Time `json:"closed_at,omitempty"`
	CreatedAt      time.Time `json:"created_at,omitempty"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
	Customer       *Customer `json:"client,omitempty"`
	Amount         float64   `json:"amount,omitempty"`
	DueAmount      float64   `json:"due_amount,omitempty"`
	Tax            float64   `json:"tax,omitempty"`
	TaxAmount      float64   `json:"tax_amount,omitempty"`
	Tax2           float64   `json:"tax2,omitempty"`
	Tax2Amount     float64   `json:"tax2_amount,omitempty"`
	Discount       float64   `json:"discount,omitempty"`
	DiscountAmount float64   `json:"discount_amount,omitempty"`
	Subject        string    `json:"subject,omitempty"`
	Notes          string    `json:"notes,omitempty"`
	Currency       string    `json:"currency,omitempty"`

	PeriodStart    string   `json:"period_start,omitempty"`
	PeriodEnd      string   `json:"period_end,omitempty"`
	IssueDate      string   `json:"issue_date,omitempty"`
	DueDate        string   `json:"due_date,omitempty"`
	PaymentTerm    string   `json:"payment_term,omitempty"`
	PaymentOptions []string `json:"payment_options"`
	PaidDate       string   `json:"paid_date,omitempty"`

	LineItems []*LineItem `json:"line_items,omitempty"`

	Hv *Client `json:"-"`
}

type Expense struct {
	ID int64 `json:"id"`

	// An object containing the associated project’s id, name, and code.
	Project *Project `json:"project"`

	SpentDate string  `json:"spent_date"`
	Notes     string  `json:"notes"`
	TotalCost float64 `json:"total_cost"`

	Hv *Client `json:"-"`
}

type Attachment struct {
	Path     string
	Filename string

	hv *Client
}

type LineItem struct {
	// Unique ID for the line item.
	ID int64 `json:"id,omitempty"`

	// An object containing the associated project’s id, name, and code.
	Project *Project `json:"project,omitempty"`

	// The name of an invoice item category.
	Kind string `json:"kind,omitempty"`

	// Text description of the line item.
	Description string `json:"description,omitempty"`

	// The unit quantity of the item.
	Quantity float64 `json:"quantity,omitempty"`

	// The individual price per unit.
	UnitPrice float64 `json:"unit_price,omitempty"`

	// The line item subtotal (quantity * unit_price).
	Amount float64 `json:"amount,omitempty"`

	// Whether the invoice’s tax percentage applies to this line item.
	Taxed bool `json:"taxed,omitempty"`

	// Whether the invoice’s tax2 percentage applies to this line item.
	Taxed2 bool `json:"taxed_2,omitempty"`
}

type Project struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Code string `json:"code"`
}

type Customer struct {
	ID   int64  `json:"id,omitempty"`
	Name string `json:"name,omitempty"`

	Hv *Client `json:"-"`
}

type Recipient struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type Result[T any] struct {
	client *Client
}

func New(accountID int64, token string) (*Client, error) {
	cookieJar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Jar: cookieJar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return &Client{
		accountID: accountID,
		token:     token,
		client:    client,
		bucket:    ratelimit.NewBucket(15*time.Second/100, 100),
	}, nil
}

func (hv *Client) GetCompanyInfo() (*Company, error) {
	if hv.company != nil {
		return hv.company, nil
	}

	req, err := http.NewRequest("GET", serverUrl+"/company", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Harvest-Account-ID", strconv.FormatInt(hv.accountID, 10))
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", hv.token))

	hv.bucket.Wait(1)
	resp, err := hv.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Failed to load company info: %d", resp.StatusCode)
	}

	info := &Company{}
	err = json.NewDecoder(resp.Body).Decode(info)
	if err != nil {
		return nil, err
	}

	hv.company = info
	return info, nil
}

func (hv *Client) Invoices(opts ...requestOption) iter.Seq2[*Invoice, error] {
	return fetchIter[Invoice](hv, "invoices", "invoices", opts)
}

func (hv *Client) Customers(opts ...requestOption) iter.Seq2[*Customer, error] {
	return fetchIter[Customer](hv, "customers", "customers", opts)
}

func (hv *Client) Expenses(opts ...requestOption) iter.Seq2[*Expense, error] {
	return fetchIter[Expense](hv, "expenses", "expenses", opts)
}

func fetchIter[T any](hv *Client, field, path string, opts []requestOption) iter.Seq2[*T, error] {
	v := &url.Values{}
	for _, o := range opts {
		o(v)
	}
	url := fmt.Sprintf("%s/%s?%s", serverUrl, path, v.Encode())

	var buf []*T
	return func(yield func(*T, error) bool) {
		for {
			if len(buf) == 0 && url != "" {
				items, next, err := fetchAll[T](hv, url, field)
				if err != nil {
					if !yield(nil, err) {
						return
					}
				}
				buf = items
				url = next
			}

			if len(buf) == 0 {
				return
			}

			obj := buf[0]
			buf = buf[1:]

			if !yield(obj, nil) {
				return
			}
		}
	}
}

func fetchAll[T any](hv *Client, url, field string) ([]*T, string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Harvest-Account-ID", strconv.FormatInt(hv.accountID, 10))
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", hv.token))

	hv.bucket.Wait(1)
	resp, err := hv.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("Failed to load %s: %d", url, resp.StatusCode)
	}

	r := make(map[string]json.RawMessage)

	err = json.NewDecoder(resp.Body).Decode(&r)
	if err != nil {
		return nil, "", err
	}

	var links struct {
		Next string `json:"next"`
	}

	err = json.Unmarshal(r["links"], &links)
	if err != nil {
		return nil, "", err
	}

	var results []*T
	err = json.Unmarshal(r[field], &results)
	if err != nil {
		return nil, "", err
	}

	c := reflect.ValueOf(hv)
	for _, obj := range results {
		v := reflect.ValueOf(obj).Elem()
		v.FieldByName("Hv").Set(c)
	}

	return results, links.Next, nil
}

func (hv *Client) FetchCustomers(opts ...requestOption) ([]*Customer, error) {
	v := &url.Values{}
	for _, o := range opts {
		o(v)
	}
	result, _, err := fetchAll[Customer](hv, fmt.Sprintf("%s/customers?%s", serverUrl, v.Encode()), "customers")
	return result, err
}

func (hv *Client) FetchInvoices(opts ...requestOption) ([]*Invoice, error) {
	v := &url.Values{}
	for _, o := range opts {
		o(v)
	}
	result, _, err := fetchAll[Invoice](hv, fmt.Sprintf("%s/invoices?%s", serverUrl, v.Encode()), "invoices")
	return result, err
}

func (hv *Client) GetRecipients(customer int64) ([]*Recipient, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/contacts?client_id=%d", serverUrl, customer), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Harvest-Account-ID", strconv.FormatInt(hv.accountID, 10))
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", hv.token))

	hv.bucket.Wait(1)
	resp, err := hv.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Failed to load invoices: %d", resp.StatusCode)
	}

	var r struct {
		Contacts []struct {
			Email     string `json:"email"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
		} `json:"contacts"`
	}

	err = json.NewDecoder(resp.Body).Decode(&r)
	if err != nil {
		return nil, err
	}

	result := make([]*Recipient, 0)
	for _, c := range r.Contacts {
		result = append(result, &Recipient{
			Email: c.Email,
			Name:  fmt.Sprintf("%s %s", c.FirstName, c.LastName),
		})
	}
	return result, nil
}

type createMessageRequest struct {
	Recipients  []*Recipient `json:"recipients"`
	SendCopy    bool         `json:"send_me_a_copy"`
	IncludeLink bool         `json:"include_link_to_client_invoice"`
	AttachPDF   bool         `json:"attach_pdf"`
	Subject     string       `json:"subject"`
	Body        string       `json:"body"`
}

func (i *Invoice) Send(subject, body string, to []*Recipient) error {
	data, err := json.Marshal(createMessageRequest{
		Recipients:  to,
		SendCopy:    true,
		IncludeLink: true,
		AttachPDF:   true,
		Subject:     subject,
		Body:        body,
	})
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/invoices/%d/messages", serverUrl, i.ID)
	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-type", "application/json")
	req.Header.Set("Harvest-Account-ID", strconv.FormatInt(i.Hv.accountID, 10))
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", i.Hv.token))

	i.Hv.bucket.Wait(1)
	resp, err := i.Hv.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("Failed to send invoice: %d", resp.StatusCode)
	}

	return nil
}

type markSentRequest struct {
	EventType string `json:"event_type"`
}

func (i *Invoice) MarkSent() error {
	data, err := json.Marshal(markSentRequest{
		EventType: "send",
	})
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/invoices/%d/messages", serverUrl, i.ID)
	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-type", "application/json")
	req.Header.Set("Harvest-Account-ID", strconv.FormatInt(i.Hv.accountID, 10))
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", i.Hv.token))

	i.Hv.bucket.Wait(1)
	resp, err := i.Hv.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("Failed to mark invoice as sent: %d", resp.StatusCode)
	}

	return nil
}

type createPaymentRequest struct {
	Amount   float64 `json:"amount"`
	PaidDate string  `json:"paid_date"`
	Notes    string  `json:"notes"`
}

func (i *Invoice) AddPayment(amount float64, date time.Time, notes string) error {
	data, err := json.Marshal(createPaymentRequest{
		Amount:   amount,
		PaidDate: date.Format("2006-01-02"),
		Notes:    notes,
	})
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/invoices/%d/payments", serverUrl, i.ID)
	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-type", "application/json")
	req.Header.Set("Harvest-Account-ID", strconv.FormatInt(i.Hv.accountID, 10))
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", i.Hv.token))

	i.Hv.bucket.Wait(1)
	resp, err := i.Hv.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("Failed to add payment: %d", resp.StatusCode)
	}

	return nil
}

func (i *Invoice) Download() (io.ReadCloser, error) {
	info, err := i.Hv.GetCompanyInfo()
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/client/invoices/%s.pdf", info.BaseURI, i.ClientKey)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	i.Hv.bucket.Wait(1)
	resp, err := i.Hv.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("Failed to download PDF: %d", resp.StatusCode)
	}
	return resp.Body, nil
}

func (i *Invoice) GetAttachments() ([]*Attachment, error) {
	info, err := i.Hv.GetCompanyInfo()
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/client/invoices/%s", info.BaseURI, i.ClientKey)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	i.Hv.bucket.Wait(1)
	resp, err := i.Hv.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Failed to fetch attachments: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	result := make([]*Attachment, 0)
	doc.Find("#document-attachments li a").Each(func(_ int, s *goquery.Selection) {
		result = append(result, &Attachment{
			Path:     s.AttrOr("href", ""),
			Filename: s.Text(),
			hv:       i.Hv,
		})
	})

	return result, nil
}

func (a *Attachment) Download() (io.ReadCloser, error) {
	info, err := a.hv.GetCompanyInfo()
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s%s", info.BaseURI, a.Path)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	a.hv.bucket.Wait(1)
	resp, err := a.hv.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("Failed to download attachment: %d", resp.StatusCode)
	}
	return resp.Body, nil
}

func (hv *Client) FetchExpenses(opts ...requestOption) ([]*Expense, error) {
	v := &url.Values{}
	for _, o := range opts {
		o(v)
	}
	result, _, err := fetchAll[Expense](hv, fmt.Sprintf("%s/expenses?%s", serverUrl, v.Encode()), "expenses")
	return result, err
}

type CreateExpense struct {
	ProjectID         int64
	ExpenseCategoryID int64
	SpentDate         string
	TotalCost         float64
	Notes             string

	Filename    string
	ContentType string
	File        io.Reader
}

func (hv *Client) CreateExpense(e *CreateExpense) error {
	pr, pw := io.Pipe()
	mp := multipart.NewWriter(pw)

	var g errgroup.Group
	g.Go(func() error {
		defer pw.Close()

		for _, f := range []struct {
			Field string
			Value string
		}{
			{"spent_date", e.SpentDate},
			{"project_id", strconv.FormatInt(e.ProjectID, 10)},
			{"expense_category_id", strconv.FormatInt(e.ExpenseCategoryID, 10)},
			{"notes", e.Notes},
			{"total_cost", fmt.Sprintf("%g", e.TotalCost)},
		} {
			fw, err := mp.CreateFormField(f.Field)
			if err != nil {
				return err
			}

			_, err = io.WriteString(fw, f.Value)
			if err != nil {
				return err
			}
		}

		if e.File != nil {
			h := textproto.MIMEHeader{}
			h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="receipt"; filename="%s"`, e.Filename))
			h.Set("Content-Type", e.ContentType)

			fw, err := mp.CreatePart(h)
			if err != nil {
				return err
			}

			_, err = io.Copy(fw, e.File)
			if err != nil {
				return err
			}
		}

		return mp.Close()
	})
	g.Go(func() (err error) {
		defer func() {
			if err != nil {
				_, _ = io.Copy(io.Discard, pr)
			}
		}()

		req, err := http.NewRequest("POST", fmt.Sprintf("%s/expenses", serverUrl), pr)
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", mp.FormDataContentType())
		req.Header.Set("Harvest-Account-ID", strconv.FormatInt(hv.accountID, 10))
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", hv.token))

		hv.bucket.Wait(1)
		resp, err := hv.client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("Failed to create expense (%d): %s", resp.StatusCode, string(body))
		}

		return nil
	})

	return g.Wait()
}

func (hv *Client) CreateInvoice(invoice *Invoice) error {
	data, err := json.Marshal(invoice)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/invoices", serverUrl)
	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-type", "application/json")
	req.Header.Set("Harvest-Account-ID", strconv.FormatInt(hv.accountID, 10))
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", hv.token))

	hv.bucket.Wait(1)
	resp, err := hv.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("Failed to create invoice: %d", resp.StatusCode)
	}

	return nil
}
