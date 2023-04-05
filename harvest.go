package harvest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/textproto"
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
	ID             int64     `json:"id"`
	ClientKey      string    `json:"client_key"`
	Number         string    `json:"number"`
	PurchaseOrder  string    `json:"purchase_order"`
	State          string    `json:"state"`
	SentAt         time.Time `json:"sent_at"`
	PaidAt         time.Time `json:"paid_at"`
	ClosedAt       time.Time `json:"closed_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Customer       *Customer `json:"client"`
	Amount         float64   `json:"amount"`
	DueAmount      float64   `json:"due_amount"`
	Tax            float64   `json:"tax"`
	TaxAmount      float64   `json:"tax_amount"`
	Tax2           float64   `json:"tax2"`
	Tax2Amount     float64   `json:"tax2_amount"`
	Discount       float64   `json:"discount"`
	DiscountAmount float64   `json:"discount_amount"`
	Subject        string    `json:"subject"`
	Notes          string    `json:"notes"`
	Currency       string    `json:"currency"`

	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
	IssueDate   string `json:"issue_date"`
	DueDate     string `json:"due_date"`
	PaymentTerm string `json:"payment_term"`
	PaidDate    string `json:"paid_date"`

	LineItems []*LineItem `json:"line_items"`

	hv *Client `json:"-"`
}

type Expense struct {
	ID int64 `json:"id"`

	// An object containing the associated project’s id, name, and code.
	Project *Project `json:"project"`

	SpentDate string  `json:"spent_date"`
	Notes     string  `json:"notes"`
	TotalCost float64 `json:"total_cost"`

	hv *Client `json:"-"`
}

type Attachment struct {
	Path     string
	Filename string

	hv *Client
}

type LineItem struct {
	// Unique ID for the line item.
	ID int64 `json:"id"`

	// An object containing the associated project’s id, name, and code.
	Project *Project `json:"project"`

	// The name of an invoice item category.
	Kind string `json:"kind"`

	// Text description of the line item.
	Description string `json:"description"`

	// The unit quantity of the item.
	Quantity float64 `json:"quantity"`

	// The individual price per unit.
	UnitPrice float64 `json:"unit_price"`

	// The line item subtotal (quantity * unit_price).
	Amount float64 `json:"amount"`

	// Whether the invoice’s tax percentage applies to this line item.
	Taxed bool `json:"taxed"`

	// Whether the invoice’s tax2 percentage applies to this line item.
	Taxed2 bool `json:"taxed_2"`
}

type Project struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Code string `json:"code"`
}

type Customer struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Recipient struct {
	Name  string `json:"name"`
	Email string `json:"email"`
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

func (hv *Client) FetchInvoices() ([]*Invoice, error) {
	req, err := http.NewRequest("GET", serverUrl+"/invoices", nil)
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
		Invoices []*Invoice `json:"invoices"`
	}

	err = json.NewDecoder(resp.Body).Decode(&r)
	if err != nil {
		return nil, err
	}

	for _, inv := range r.Invoices {
		inv.hv = hv
	}

	return r.Invoices, nil
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
	req.Header.Set("Harvest-Account-ID", strconv.FormatInt(i.hv.accountID, 10))
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", i.hv.token))

	i.hv.bucket.Wait(1)
	resp, err := i.hv.client.Do(req)
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
	req.Header.Set("Harvest-Account-ID", strconv.FormatInt(i.hv.accountID, 10))
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", i.hv.token))

	i.hv.bucket.Wait(1)
	resp, err := i.hv.client.Do(req)
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
	req.Header.Set("Harvest-Account-ID", strconv.FormatInt(i.hv.accountID, 10))
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", i.hv.token))

	i.hv.bucket.Wait(1)
	resp, err := i.hv.client.Do(req)
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
	info, err := i.hv.GetCompanyInfo()
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/client/invoices/%s.pdf", info.BaseURI, i.ClientKey)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	i.hv.bucket.Wait(1)
	resp, err := i.hv.client.Do(req)
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
	info, err := i.hv.GetCompanyInfo()
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/client/invoices/%s", info.BaseURI, i.ClientKey)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	i.hv.bucket.Wait(1)
	resp, err := i.hv.client.Do(req)
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
			hv:       i.hv,
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

func (hv *Client) FetchExpenses() ([]*Expense, error) {
	req, err := http.NewRequest("GET", serverUrl+"/expenses", nil)
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
		return nil, fmt.Errorf("Failed to load expenses: %d", resp.StatusCode)
	}

	var r struct {
		Expenses []*Expense `json:"expenses"`
	}

	err = json.NewDecoder(resp.Body).Decode(&r)
	if err != nil {
		return nil, err
	}

	for _, exp := range r.Expenses {
		exp.hv = hv
	}

	return r.Expenses, nil
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
