package models

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// APEX Billing data. Nothing is ever hard-deleted: clients become
// "archived", users "disabled", so history (invoices, payments, the audit
// log) always points at a real record.

// Roles. Fixed for now (no permission matrix):
//
//	admin       everything: users, settings, clients, billing
//	accountant  clients, products, invoices, payments, reports
//	recorder    sees clients and invoices; records payments (with slip)
//	verifier    sees clients and invoices; verifies / rejects payments
const (
	RoleAdmin      = "admin"
	RoleAccountant = "accountant"
	RoleRecorder   = "recorder"
	RoleVerifier   = "verifier"
)

var Roles = map[string]string{
	RoleAdmin:      "Admin",
	RoleAccountant: "Accountant",
	RoleRecorder:   "Payment Recorder",
	RoleVerifier:   "Payment Verifier",
}

// User — someone who signs in to APEX Billing (separate from APEX CORE).
type User struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	UserNumber string `gorm:"size:20;index" json:"user_number"` // BU-000001
	Name       string `gorm:"size:150;not null" json:"name"`
	Email      string `gorm:"size:200;not null;uniqueIndex" json:"email"` // stored lower-case
	Phone      string `gorm:"size:40" json:"phone"`
	Role       string `gorm:"size:20;not null;default:'recorder'" json:"role"`
	Active     bool   `gorm:"not null;default:true" json:"active"`

	PasswordHash      string     `gorm:"size:200;not null" json:"-"`
	PasswordChangedAt *time.Time `json:"-"`
	// Set when an admin sets the password: the user picks a new one at
	// their next sign-in.
	MustChangePassword bool       `gorm:"not null;default:false" json:"must_change_password"`
	LastLoginAt        *time.Time `json:"last_login_at"`

	// Two-step sign-in (authenticator app). The key is stored encrypted
	// with APP_SECRET; recovery codes only as hashes.
	TOTPEnabled    bool   `gorm:"column:totp_enabled;not null;default:false" json:"totp_enabled"`
	TOTPSecret     string `gorm:"column:totp_secret;size:200" json:"-"`
	TOTPPending    string `gorm:"column:totp_pending;size:200" json:"-"` // being set up, not confirmed yet
	TOTPLastStep   int64  `gorm:"column:totp_last_step;not null;default:0" json:"-"`
	RecoveryHashes string `gorm:"type:text" json:"-"` // JSON list of unused recovery-code hashes

	// Profile picture, stored under uploads/avatars/ (PNG, JPG or WEBP).
	AvatarPath string `gorm:"size:300" json:"-"`
	AvatarType string `gorm:"size:50" json:"-"`
}

// Session — one signed-in browser. Only a hash of the cookie's token is
// stored. Ended sessions keep their row (who was signed in, from where).
type Session struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	CreatedAt    time.Time  `json:"created_at"`
	TokenHash    string     `gorm:"size:64;not null;uniqueIndex" json:"-"`
	UserID       uint       `gorm:"not null;index" json:"user_id"`
	LastSeenAt   time.Time  `json:"last_seen_at"`
	ExpiresAt    time.Time  `json:"expires_at"`
	IP           string     `gorm:"size:64" json:"ip"`
	UserAgent    string     `gorm:"size:300" json:"user_agent"`
	RevokedAt    *time.Time `gorm:"index" json:"revoked_at"`
	RevokeReason string     `gorm:"size:60" json:"revoke_reason"`
}

func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.UserNumber == "" {
		id, err := NextID(tx, "BU")
		if err != nil {
			return err
		}
		u.UserNumber = id
	}
	return nil
}

// AuditLog — who did what, when, from where. Rows are never changed.
type AuditLog struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	CreatedAt  time.Time `gorm:"index" json:"created_at"`
	UserID     uint      `gorm:"index" json:"user_id"`
	UserName   string    `gorm:"size:150" json:"user_name"`
	Action     string    `gorm:"size:40;index" json:"action"`      // created, updated, archived, login, ...
	EntityType string    `gorm:"size:30;index" json:"entity_type"` // client, user, settings, ...
	EntityID   uint      `gorm:"index" json:"entity_id"`
	Details    string    `gorm:"type:text" json:"details"`
	IP         string    `gorm:"size:64" json:"ip"`
}

// CompanySettings — one row (ID 1): the defaults for new clients and
// invoices and the security switches. The company fields here are from
// before there could be several companies: they were copied into the first
// Company at the upgrade and are no longer edited.
type CompanySettings struct {
	ID        uint      `gorm:"primaryKey" json:"-"`
	UpdatedAt time.Time `json:"updated_at"`

	CompanyName       string  `gorm:"size:200" json:"company_name"`
	Address           string  `gorm:"type:text" json:"address"`
	Phone             string  `gorm:"size:60" json:"phone"`
	Email             string  `gorm:"size:200" json:"email"`
	Website           string  `gorm:"size:200" json:"website"`
	NTN               string  `gorm:"column:ntn;size:40" json:"ntn"`   // National Tax Number
	STRN              string  `gorm:"column:strn;size:40" json:"strn"` // Sales Tax Registration Number
	BankDetails       string  `gorm:"type:text" json:"bank_details"`   // printed on invoices: "Pay to"
	InvoiceFooter     string  `gorm:"type:text" json:"invoice_footer"`
	InvoicePrefix     string  `gorm:"size:10;not null;default:'INV'" json:"invoice_prefix"`
	Currency          string  `gorm:"size:10;not null;default:'PKR'" json:"currency"`
	DefaultTermsDays  int     `gorm:"not null;default:15" json:"default_terms_days"` // due date = invoice date + this
	DefaultTaxPercent float64 `gorm:"type:numeric(5,2);not null;default:0" json:"default_tax_percent"`
	// Everyone must use two-step sign-in (authenticator app).
	Require2FA  bool `gorm:"column:require_2fa;not null;default:false" json:"require_2fa"`
	UpdatedByID uint `json:"updated_by_id"`
}

// Company — one of our own companies that bills clients. Each has its own
// details, logo and bank details (printed on its invoices), its own invoice
// number prefix and counter, and its own taxes. Each service says which
// company bills it, so one client can get invoices from several companies.
// One company is the default (for new services). Never deleted: made
// inactive.
type Company struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Name          string `gorm:"size:200;not null" json:"name"`
	Address       string `gorm:"type:text" json:"address"`
	Phone         string `gorm:"size:60" json:"phone"`
	Email         string `gorm:"size:200" json:"email"`
	Website       string `gorm:"size:200" json:"website"`
	NTN           string `gorm:"column:ntn;size:40" json:"ntn"`
	STRN          string `gorm:"column:strn;size:40" json:"strn"`
	BankDetails   string `gorm:"type:text" json:"bank_details"`
	InvoiceFooter string `gorm:"type:text" json:"invoice_footer"`
	// Invoice numbers: PREFIX-000001; each company has its own prefix
	// (unique) and counter.
	InvoicePrefix string `gorm:"size:10;not null" json:"invoice_prefix"`

	// The logo printed on invoices: a PNG / JPG / WEBP in UPLOAD_DIR/logos/.
	// A replaced logo's file is kept (issued invoices still point at it).
	LogoPath string `gorm:"size:300" json:"-"`
	LogoName string `gorm:"size:200" json:"logo_name"`
	LogoType string `gorm:"size:60" json:"-"`
	LogoSize int64  `json:"logo_size"`

	IsDefault bool `gorm:"not null;default:false" json:"is_default"`
	Active    bool `gorm:"not null;default:true;index" json:"active"`

	CreatedByID uint `json:"created_by_id"`
	UpdatedByID uint `json:"updated_by_id"`
}

// Issuer — the billing company as printed on an invoice, copied when it is
// issued (so later changes to the company never alter an issued invoice).
type Issuer struct {
	Name          string `json:"name"`
	Address       string `json:"address"`
	Phone         string `json:"phone"`
	Email         string `json:"email"`
	Website       string `json:"website"`
	NTN           string `json:"ntn"`
	STRN          string `json:"strn"`
	BankDetails   string `json:"bank_details"`
	InvoiceFooter string `json:"invoice_footer"`
}

// Client — a customer we bill. Its billing details are copied onto each
// invoice when it is issued, so a later change never alters an issued
// invoice.
type Client struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	ClientNumber string `gorm:"size:20;index" json:"client_number"` // CUS-000001, permanent

	// company: a business (contact person, NTN / STRN). individual: a
	// person billed in their own name (CNIC; NTN only if they have one).
	ClientType string `gorm:"size:20;not null;default:'company';index" json:"client_type"`

	Name          string `gorm:"size:200;not null;index" json:"name"` // company name, or the person's full name
	ContactPerson string `gorm:"size:150" json:"contact_person"`
	Email         string `gorm:"size:200" json:"email"` // billing email (invoices go here)
	Phone         string `gorm:"size:60" json:"phone"`
	Mobile        string `gorm:"size:60" json:"mobile"`
	Address       string `gorm:"type:text" json:"address"`
	City          string `gorm:"size:100;index" json:"city"`
	Province      string `gorm:"size:40" json:"province"` // tax is often provincial
	NTN           string `gorm:"column:ntn;size:40" json:"ntn"`
	STRN          string `gorm:"column:strn;size:40" json:"strn"`
	CNIC          string `gorm:"column:cnic;size:20" json:"cnic"`

	// Billing terms. TaxPercent is added on top of the invoice subtotal
	// (0 when rates already include tax). Due date = invoice date + TermsDays.
	TaxPercent float64 `gorm:"type:numeric(5,2);not null;default:0" json:"tax_percent"`
	TermsDays  int     `gorm:"not null;default:15" json:"terms_days"`

	// What the client already owed when they were added (moved over from an
	// older system), and as of which date. Counted in their balance.
	OpeningBalance     float64    `gorm:"type:numeric(14,2);not null;default:0" json:"opening_balance"`
	OpeningBalanceDate *time.Time `gorm:"type:date" json:"opening_balance_date"`

	Notes  string `gorm:"type:text" json:"notes"`
	Status string `gorm:"size:20;not null;default:'active';index" json:"status"` // active | inactive | archived

	CreatedByID uint `json:"created_by_id"`
	UpdatedByID uint `json:"updated_by_id"`
}

const (
	ClientCompany    = "company"
	ClientIndividual = "individual"
)

var ClientTypes = map[string]string{ClientCompany: "Company", ClientIndividual: "Individual"}

func (c *Client) BeforeCreate(tx *gorm.DB) error {
	if c.ClientNumber == "" {
		id, err := NextID(tx, "CUS")
		if err != nil {
			return err
		}
		c.ClientNumber = id
	}
	return nil
}

// Charge types for products and client services.
//
//	monthly   MRC: billed every month, in advance; part months by days used
//	one_time  billed once, on the next invoice after its date
//	annual    billed once a year, from the start date
const (
	ChargeMonthly = "monthly"
	ChargeOneTime = "one_time"
	ChargeAnnual  = "annual"
)

var ChargeTypes = map[string]string{
	ChargeMonthly: "Monthly (MRC)",
	ChargeOneTime: "One-time",
	ChargeAnnual:  "Per annum",
}

// Product — a service in the catalogue (MPLS, IPT, DPLC, installation…)
// with its usual rate. A client's own rate is kept on the subscription, so
// changing a product's default rate never changes what a client pays.
type Product struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Code        string `gorm:"size:30;index" json:"code"`           // optional short code, e.g. MPLS-10M
	Name        string `gorm:"size:200;not null;index" json:"name"` // printed on invoices
	Description string `gorm:"type:text" json:"description"`
	Unit        string `gorm:"size:40" json:"unit"` // e.g. "link", "Mbps" (printed after the quantity)
	// Category — optional group heading on printed invoices, e.g.
	// "IT Services", "Servers & Hosting". Lines are grouped by it.
	Category    string  `gorm:"size:60;index" json:"category"`
	ChargeType  string  `gorm:"size:20;not null;default:'monthly'" json:"charge_type"`
	DefaultRate float64 `gorm:"type:numeric(14,2);not null;default:0" json:"default_rate"`
	Active      bool    `gorm:"not null;default:true;index" json:"active"` // inactive: can't be added to clients any more
	// Taxes a new service of this product gets (each service keeps its own).
	TaxIDs []uint `gorm:"column:tax_ids;serializer:json;type:text" json:"tax_ids"`

	CreatedByID uint `json:"created_by_id"`
	UpdatedByID uint `json:"updated_by_id"`
}

// Subscription — one service a client takes (shown as a "service"): its own
// rate, quantity and charge type, from StartDate to EndDate (open-ended
// when empty). Status "suspended" pauses billing; ending a service is
// setting its end date. Never deleted.
type Subscription struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	SubscriptionNumber string `gorm:"size:20;index" json:"subscription_number"` // SUB-000001, permanent

	ClientID  uint `gorm:"not null;index" json:"client_id"`
	ProductID uint `gorm:"not null;index" json:"product_id"`
	// The company that bills this service (its invoices come from it).
	CompanyID uint `gorm:"not null;default:0;index" json:"company_id"`

	// The line printed on the invoice (defaults to the product name), and
	// the circuit / link ID or site that tells this service apart.
	Description string `gorm:"size:300;not null" json:"description"`
	Reference   string `gorm:"size:100" json:"reference"`

	ChargeType string  `gorm:"size:20;not null" json:"charge_type"`
	Quantity   float64 `gorm:"type:numeric(12,2);not null;default:1" json:"quantity"`
	Rate       float64 `gorm:"type:numeric(14,2);not null;default:0" json:"rate"` // per month / once / per year, per unit

	// StartDate: service starts (monthly / annual) or the charge date
	// (one-time). EndDate: last day billed.
	StartDate time.Time  `gorm:"type:date;not null;index" json:"start_date"`
	EndDate   *time.Time `gorm:"type:date;index" json:"end_date"`

	Status string `gorm:"size:20;not null;default:'active';index" json:"status"` // active | suspended

	// Last day already invoiced: the latest period end on an issued
	// invoice (worked out again whenever an invoice is issued or
	// cancelled). Empty = never billed.
	BilledThrough *time.Time `gorm:"type:date" json:"billed_through"`

	// Don't bill any day before this date. For services moved over from an
	// older system (bill from the first month invoiced here) and set
	// automatically when a suspended service is resumed, so the suspended
	// time isn't billed. Empty = bill from the start date.
	BillFrom *time.Time `gorm:"type:date" json:"bill_from"`

	Notes string `gorm:"type:text" json:"notes"`

	// The contract this service falls under, and its service level (both
	// optional). The SLA decides uptime / response targets and credits.
	ContractID *uint `gorm:"index" json:"contract_id"`
	SLAPlanID  *uint `gorm:"column:sla_plan_id;index" json:"sla_plan_id"`

	// Taxes on this service's charges (from the product when added).
	TaxIDs []uint `gorm:"column:tax_ids;serializer:json;type:text" json:"tax_ids"`

	CreatedByID uint `json:"created_by_id"`
	UpdatedByID uint `json:"updated_by_id"`

	Client   *Client   `gorm:"foreignKey:ClientID" json:"client,omitempty"`
	Product  *Product  `gorm:"foreignKey:ProductID" json:"product,omitempty"`
	Company  *Company  `gorm:"foreignKey:CompanyID;-:migration" json:"company,omitempty"`
	Contract *Contract `gorm:"foreignKey:ContractID" json:"contract,omitempty"`
	SLAPlan  *SLAPlan  `gorm:"foreignKey:SLAPlanID" json:"sla_plan,omitempty"`
}

func (s *Subscription) BeforeCreate(tx *gorm.DB) error {
	if s.SubscriptionNumber == "" {
		id, err := NextID(tx, "SUB")
		if err != nil {
			return err
		}
		s.SubscriptionNumber = id
	}
	return nil
}

// ---- service levels and contracts ----------------------------------------

// Support hours an SLA covers.
var SupportHours = map[string]string{
	"24x7":     "24 × 7",
	"business": "Business hours",
	"custom":   "Other (see note)",
}

// SLACreditTier: when a month's uptime falls below BelowPercent, the client
// is owed CreditPercent of that service's monthly charge. The lowest tier
// that applies wins.
type SLACreditTier struct {
	BelowPercent  float64 `json:"below_percent"`
	CreditPercent float64 `json:"credit_percent"`
}

// SLAPlan — a service level (e.g. "Gold: 99.9%, 2-hour restore"). Each
// service can have one; a contract can give a default for its services.
// Never deleted: made inactive when no longer offered.
type SLAPlan struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Name string `gorm:"size:100;not null;index" json:"name"`
	Code string `gorm:"size:30" json:"code"`

	UptimeTarget    float64 `gorm:"type:numeric(6,3);not null;default:0" json:"uptime_target"` // % a month, e.g. 99.5
	ResponseMinutes int     `gorm:"not null;default:0" json:"response_minutes"`                // first response to a reported fault
	RestoreMinutes  int     `gorm:"not null;default:0" json:"restore_minutes"`                 // service restored (MTTR target)
	SupportHours    string  `gorm:"size:20;not null;default:'24x7'" json:"support_hours"`
	SupportNote     string  `gorm:"size:200" json:"support_note"` // e.g. "Mon–Sat 9:00–18:00"

	CreditTiers      []SLACreditTier `gorm:"serializer:json;type:text" json:"credit_tiers"`
	MaxCreditPercent float64         `gorm:"type:numeric(5,2);not null;default:0" json:"max_credit_percent"` // cap per month; 0 = no cap

	Description string `gorm:"type:text" json:"description"` // terms, exclusions (planned maintenance…)
	Active      bool   `gorm:"not null;default:true;index" json:"active"`

	CreatedByID uint `json:"created_by_id"`
	UpdatedByID uint `json:"updated_by_id"`
}

func (SLAPlan) TableName() string { return "sla_plans" }

// Contract statuses (stored). What a contract means today (upcoming,
// expiring, expired, auto-renewed…) is worked out from its dates.
//
//	draft       being prepared / not signed yet
//	active      signed and in force
//	terminated  ended early (or not renewed) on TerminatedOn
const (
	ContractDraft      = "draft"
	ContractActive     = "active"
	ContractTerminated = "terminated"
)

// Contract — an agreement with a client covering some of its services: the
// term (start, end, auto-renewal, notice), the default service level and
// the signed copy. Services point at their contract. Never deleted.
type Contract struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	ContractNumber string `gorm:"size:20;index" json:"contract_number"` // CON-000001, permanent
	ClientID       uint   `gorm:"not null;index" json:"client_id"`

	Title           string `gorm:"size:200;not null" json:"title"`
	ClientReference string `gorm:"size:100" json:"client_reference"` // their PO / agreement number

	SignedDate *time.Time `gorm:"type:date" json:"signed_date"`
	StartDate  time.Time  `gorm:"type:date;not null;index" json:"start_date"`
	// End of the (first) term. Empty = no fixed end. With AutoRenew, the
	// term rolls on by RenewMonths each time it reaches its end.
	EndDate     *time.Time `gorm:"type:date;index" json:"end_date"`
	AutoRenew   bool       `gorm:"not null;default:false" json:"auto_renew"`
	RenewMonths int        `gorm:"not null;default:12" json:"renew_months"`
	NoticeDays  int        `gorm:"not null;default:30" json:"notice_days"` // notice to end / not renew

	SLAPlanID *uint `gorm:"column:sla_plan_id;index" json:"sla_plan_id"` // default SLA for its services

	Status          string     `gorm:"size:20;not null;default:'active';index" json:"status"`
	TerminatedOn    *time.Time `gorm:"type:date" json:"terminated_on"`
	TerminateReason string     `gorm:"type:text" json:"terminate_reason"`
	TerminatedByID  uint       `json:"terminated_by_id"`

	Notes string `gorm:"type:text" json:"notes"`

	// The signed copy (PDF or scan), stored in UPLOAD_DIR/contracts/.
	DocPath string `gorm:"size:300" json:"-"`
	DocName string `gorm:"size:200" json:"doc_name"`
	DocType string `gorm:"size:60" json:"doc_type"`
	DocSize int64  `json:"doc_size"`

	CreatedByID uint `json:"created_by_id"`
	UpdatedByID uint `json:"updated_by_id"`

	Client  *Client  `gorm:"foreignKey:ClientID" json:"client,omitempty"`
	SLAPlan *SLAPlan `gorm:"foreignKey:SLAPlanID" json:"sla_plan,omitempty"`
}

func (c *Contract) BeforeCreate(tx *gorm.DB) error {
	if c.ContractNumber == "" {
		id, err := NextID(tx, "CON")
		if err != nil {
			return err
		}
		c.ContractNumber = id
	}
	return nil
}

// Invoice statuses. Payment states (partly paid, paid) come from verified
// payments in step 4.
//
//	draft      being prepared; no number yet; can be edited or deleted
//	issued     final: numbered, client details copied, can't change
//	cancelled  an issued invoice withdrawn (keeps its number, never reused)
const (
	InvoiceDraft     = "draft"
	InvoiceIssued    = "issued"
	InvoiceCancelled = "cancelled"
)

// Invoice — one bill to one client. A draft shows the client's current
// details; issuing copies them onto the invoice, so later changes to the
// client never alter an issued invoice.
type Invoice struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	InvoiceNumber string `gorm:"size:20;index" json:"invoice_number"` // INV-000001, given at issue
	ClientID      uint   `gorm:"not null;index" json:"client_id"`
	CompanyID     uint   `gorm:"not null;default:0;index" json:"company_id"` // the company that bills
	Status        string `gorm:"size:20;not null;default:'draft';index" json:"status"`

	// BillingMonth: first day of the month a bulk run billed (empty for a
	// manual invoice).
	BillingMonth *time.Time `gorm:"type:date;index" json:"billing_month"`
	InvoiceDate  time.Time  `gorm:"type:date;not null;index" json:"invoice_date"`
	DueDate      time.Time  `gorm:"type:date;not null;index" json:"due_date"`

	// Client details as printed (copied at issue).
	BillClientType string `gorm:"size:20" json:"bill_client_type"` // company | individual (empty on older invoices = company)
	BillName       string `gorm:"size:200" json:"bill_name"`
	BillContact    string `gorm:"size:150" json:"bill_contact"`
	BillEmail      string `gorm:"size:200" json:"bill_email"`
	BillPhone      string `gorm:"size:60" json:"bill_phone"`
	BillAddress    string `gorm:"type:text" json:"bill_address"`
	BillCity       string `gorm:"size:100" json:"bill_city"`
	BillProvince   string `gorm:"size:40" json:"bill_province"`
	BillNTN        string `gorm:"column:bill_ntn;size:40" json:"bill_ntn"`
	BillSTRN       string `gorm:"column:bill_strn;size:40" json:"bill_strn"`
	BillCNIC       string `gorm:"column:bill_cnic;size:20" json:"bill_cnic"`

	// The billing company as printed (copied at issue; empty on drafts and
	// on invoices issued before there were several companies), and the
	// logo file it had then.
	Issuer         Issuer `gorm:"serializer:json;type:text" json:"issuer"`
	IssuerLogoPath string `gorm:"size:300" json:"-"`
	IssuerLogoType string `gorm:"size:60" json:"-"`

	Currency string  `gorm:"size:10;not null;default:'PKR'" json:"currency"`
	Subtotal float64 `gorm:"type:numeric(14,2);not null;default:0" json:"subtotal"`
	// TaxPercent: the old single tax % on the whole subtotal (invoices made
	// before taxes were set per service; 0 on new ones).
	TaxPercent float64 `gorm:"type:numeric(5,2);not null;default:0" json:"tax_percent"`
	TaxAmount  float64 `gorm:"type:numeric(14,2);not null;default:0" json:"tax_amount"` // taxes added
	// Taxes deducted (withheld) on the invoice itself.
	DeductedAmount float64 `gorm:"type:numeric(14,2);not null;default:0" json:"deducted_amount"`
	// Total = Subtotal + TaxAmount − DeductedAmount.
	Total float64 `gorm:"type:numeric(14,2);not null;default:0" json:"total"`
	// Each tax on the invoice with its taxable amount and amount (summed
	// over the lines), as printed.
	TaxLines []TaxApplied `gorm:"serializer:json;type:text" json:"tax_lines"`
	// From verified payments (worked out again on every verify / reverse /
	// allocation). Balance = Total − AmountPaid.
	AmountPaid float64 `gorm:"type:numeric(14,2);not null;default:0" json:"amount_paid"`

	Notes string `gorm:"type:text" json:"notes"` // printed on the invoice

	IssuedAt      *time.Time `json:"issued_at"`
	IssuedByID    uint       `json:"issued_by_id"`
	CancelledAt   *time.Time `json:"cancelled_at"`
	CancelledByID uint       `json:"cancelled_by_id"`
	CancelReason  string     `gorm:"type:text" json:"cancel_reason"`

	CreatedByID uint `json:"created_by_id"`
	UpdatedByID uint `json:"updated_by_id"`

	Client  *Client       `gorm:"foreignKey:ClientID" json:"client,omitempty"`
	Company *Company      `gorm:"foreignKey:CompanyID;-:migration" json:"company,omitempty"` // no database link: company_id is filled in at startup
	Lines   []InvoiceLine `gorm:"foreignKey:InvoiceID" json:"lines,omitempty"`
}

// InvoiceLine — one charge on an invoice. Lines made from a service keep
// its id and the period they bill; a part month records the days billed
// (ProrateDays of ProrateOf), and Amount = Quantity × Rate × days ÷ month.
type InvoiceLine struct {
	ID        uint `gorm:"primaryKey" json:"id"`
	InvoiceID uint `gorm:"not null;index" json:"invoice_id"`
	SortOrder int  `gorm:"not null;default:0" json:"sort_order"`

	SubscriptionID *uint      `gorm:"index" json:"subscription_id"`
	ChargeType     string     `gorm:"size:20" json:"charge_type"` // empty for a typed-in line
	PeriodStart    *time.Time `gorm:"type:date" json:"period_start"`
	PeriodEnd      *time.Time `gorm:"type:date;index" json:"period_end"`
	ProrateDays    int        `gorm:"not null;default:0" json:"prorate_days"` // 0 = full period
	ProrateOf      int        `gorm:"not null;default:0" json:"prorate_of"`

	Description string `gorm:"size:300;not null" json:"description"`
	Reference   string `gorm:"size:100" json:"reference"`
	// Category — the heading this line is grouped under on the printed
	// invoice (e.g. "IT Services"); empty = the product's category, if any.
	Category string `gorm:"size:60" json:"category"`
	// Unit printed next to the quantity (e.g. "Month"); empty = the product's unit.
	Unit     string  `gorm:"size:40" json:"unit"`
	Quantity float64 `gorm:"type:numeric(12,2);not null;default:1" json:"quantity"`
	Rate     float64 `gorm:"type:numeric(14,2);not null;default:0" json:"rate"`
	Amount   float64 `gorm:"type:numeric(14,2);not null;default:0" json:"amount"`
	// The taxes on this line, copied with their rates when the line is
	// made or edited (a later rate change doesn't alter it).
	Taxes []TaxApplied `gorm:"serializer:json;type:text" json:"taxes"`
}

// ---- taxes -----------------------------------------------------------------

// Tax kinds.
//
//	add     added to the invoice (sales tax, FED…)
//	deduct  taken off the invoice total (withheld at source and shown on it)
const (
	TaxAdd    = "add"
	TaxDeduct = "deduct"
)

// Tax — one tax a company charges, set up by hand: as many as needed. Each
// company has its own taxes (e.g. registered in different provinces).
// Products and services choose theirs. Compound: worked out on the amount
// plus the taxes added before it (lower Order first). Never deleted; made
// inactive when no longer used.
type Tax struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	CompanyID   uint    `gorm:"not null;default:0;index" json:"company_id"`
	Name        string  `gorm:"size:100;not null;index" json:"name"` // printed, e.g. "Punjab Sales Tax on Services"
	ShortName   string  `gorm:"size:30" json:"short_name"`           // e.g. "PST" (shown on lines)
	Rate        float64 `gorm:"type:numeric(8,4);not null;default:0" json:"rate"`
	Kind        string  `gorm:"size:10;not null;default:'add'" json:"kind"`
	Compound    bool    `gorm:"not null;default:false" json:"compound"`
	Order       int     `gorm:"column:sort_order;not null;default:0" json:"order"`
	Description string  `gorm:"type:text" json:"description"`
	Active      bool    `gorm:"not null;default:true;index" json:"active"`

	CreatedByID uint `json:"created_by_id"`
	UpdatedByID uint `json:"updated_by_id"`
}

// TaxApplied — a tax as used on an invoice line (or summed for the
// invoice): a copy of the tax at that time, the amount it was worked out
// on and the result.
type TaxApplied struct {
	TaxID     uint    `json:"tax_id"` // 0: the old single tax % of an older invoice
	Name      string  `json:"name"`
	ShortName string  `json:"short_name,omitempty"`
	Rate      float64 `json:"rate"`
	Kind      string  `json:"kind"`
	Compound  bool    `json:"compound,omitempty"`
	Order     int     `json:"order"`
	Base      float64 `json:"base"`
	Amount    float64 `json:"amount"`
}

// Payment statuses.
//
//	pending   recorded with its slip, waiting for a verifier
//	verified  checked against the bank / slip: counts as paid
//	rejected  not accepted (wrong amount, slip not genuine, …)
//	reversed  was verified, then undone (e.g. a cheque bounced)
const (
	PaymentPending  = "pending"
	PaymentVerified = "verified"
	PaymentRejected = "rejected"
	PaymentReversed = "reversed"
)

var PaymentMethods = map[string]string{
	"bank_transfer": "Bank transfer / IBFT",
	"cheque":        "Cheque",
	"pay_order":     "Pay order / demand draft",
	"cash":          "Cash",
	"online":        "Online / card",
	"other":         "Other",
}

// Payment — money received from a client. Only verified payments count.
// Amount is what reached us; TaxDeducted is income tax the client withheld
// and paid to the government for us (WHT, backed by their certificate),
// which also settles the invoice. The total credited is applied to the
// client's invoices (allocations); anything not applied is the client's
// credit, usable later.
type Payment struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	PaymentNumber string `gorm:"size:20;index" json:"payment_number"` // PAY-000001
	ClientID      uint   `gorm:"not null;index" json:"client_id"`

	PaymentDate time.Time `gorm:"type:date;not null;index" json:"payment_date"`
	Method      string    `gorm:"size:20;not null" json:"method"`
	Reference   string    `gorm:"size:100" json:"reference"` // cheque no., transaction id
	BankName    string    `gorm:"size:100" json:"bank_name"` // the bank it came from
	Amount      float64   `gorm:"type:numeric(14,2);not null" json:"amount"`
	TaxDeducted float64   `gorm:"type:numeric(14,2);not null;default:0" json:"tax_deducted"`
	Notes       string    `gorm:"type:text" json:"notes"`

	// The slip (photo or PDF of the deposit slip / cheque / transfer
	// receipt), stored in UPLOAD_DIR.
	SlipPath string `gorm:"size:300" json:"-"`
	SlipName string `gorm:"size:200" json:"slip_name"`
	SlipType string `gorm:"size:60" json:"slip_type"`
	SlipSize int64  `json:"slip_size"`

	Status        string     `gorm:"size:20;not null;default:'pending';index" json:"status"`
	RecordedByID  uint       `gorm:"index" json:"recorded_by_id"`
	RecordedBy    string     `gorm:"size:150" json:"recorded_by"`
	CheckedByID   uint       `json:"checked_by_id"` // verified / rejected by
	CheckedBy     string     `gorm:"size:150" json:"checked_by"`
	CheckedAt     *time.Time `json:"checked_at"`
	RejectReason  string     `gorm:"type:text" json:"reject_reason"`
	ReversedByID  uint       `json:"reversed_by_id"`
	ReversedBy    string     `gorm:"size:150" json:"reversed_by"`
	ReversedAt    *time.Time `json:"reversed_at"`
	ReverseReason string     `gorm:"type:text" json:"reverse_reason"`

	Client      *Client             `gorm:"foreignKey:ClientID" json:"client,omitempty"`
	Allocations []PaymentAllocation `gorm:"foreignKey:PaymentID" json:"allocations,omitempty"`
}

func (p *Payment) BeforeCreate(tx *gorm.DB) error {
	if p.PaymentNumber == "" {
		id, err := NextID(tx, "PAY")
		if err != nil {
			return err
		}
		p.PaymentNumber = id
	}
	return nil
}

// PaymentAllocation — part of a payment applied to one invoice.
type PaymentAllocation struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	PaymentID   uint      `gorm:"not null;index" json:"payment_id"`
	InvoiceID   uint      `gorm:"not null;index" json:"invoice_id"`
	Amount      float64   `gorm:"type:numeric(14,2);not null" json:"amount"`
	CreatedByID uint      `json:"created_by_id"`

	Invoice *Invoice `gorm:"foreignKey:InvoiceID" json:"invoice,omitempty"`
}

// IDCounter — one row per prefix; permanent numbers (CUS-000001, INV-...)
// come from here, incremented atomically, so two records created at the
// same moment never get the same number.
type IDCounter struct {
	Prefix string `gorm:"primaryKey;size:10"`
	Last   int64  `gorm:"not null"`
}

// NextID returns the next permanent number for a prefix, e.g. "CUS-000042".
func NextID(tx *gorm.DB, prefix string) (string, error) {
	var last int64
	err := tx.Raw(`INSERT INTO id_counters (prefix, last) VALUES (?, 1)
		ON CONFLICT (prefix) DO UPDATE SET last = id_counters.last + 1
		RETURNING last`, prefix).Scan(&last).Error
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%06d", prefix, last), nil
}

// All lists every model for AutoMigrate.
func All() []interface{} {
	return []interface{}{
		&IDCounter{}, &User{}, &Session{}, &AuditLog{}, &CompanySettings{}, &Company{}, &Client{},
		&Tax{}, &Product{}, &SLAPlan{}, &Contract{}, &Subscription{}, &Invoice{}, &InvoiceLine{},
		&Payment{}, &PaymentAllocation{},
	}
}
