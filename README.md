# APEX Billing

A separate app for billing: its own users, its own PostgreSQL database, no
link to APEX CORE. Built with the same tools as APEX CORE (Go + React +
PostgreSQL), as one program that serves both the API and the web pages, so
it can run on the same server as APEX CORE (different port) or anywhere else.

## Plan

1. **Foundation** (done): users and roles, sign-in, audit log,
   company settings, billing clients.
2. **Products and services** (done): service catalogue (Products),
   each client's services with their own rate, quantity, charge type
   (monthly / one-time / annual), start / end dates, suspend / end, and
   the monthly recurring total (MRC).
3. **Invoices** (done): monthly run (drafts → review → issue all),
   manual invoices, part-month charges by days used, tax % per client,
   INV-000001 numbers given at issue, cancel with reason, printable / PDF
   invoice.
4. **Payments** (done): record with the slip, tax deducted (WHT),
   apply to invoices; verify / reject / reverse; invoices partly paid / paid
   / overdue; client balance and credit; dashboard figures.
5. **Reports** (this version): ageing, billed vs collected (with chart),
   MRC by product / client, invoice register, payment register; every
   report downloads as an Excel file.

## First start

1. Create an empty database, e.g. `createdb apex_billing` (or in pgAdmin).
2. Copy `.env.example` to `.env` and fill it in (database, `APP_SECRET`,
   first admin's email and password).
3. Build: `build.bat` (Windows) or `./build.sh` — needs Go and Node.js, the
   same versions as APEX CORE.
4. Run `apex-billing.exe` and open http://localhost:8090. Tables are created
   automatically. Sign in as the first admin and choose a new password.
5. Fill in **Company settings** (each of your companies: logo, details,
   bank details, invoice prefix — printed on its invoices), add **Users**,
   **Products** (MPLS, IPT, DPLC, installation…), then **Clients** and
   their **Services** (from the client's page or the Services page).

## If the build complains

- `go: updates to go.mod needed` or a missing package: run `go mod tidy` once, then build again.
- `npm` errors: delete `frontend/node_modules` and run the build again.

## Development

- Backend: `go run .` (port 8090 by default; set `GIN_MODE=debug` in .env).
- Web pages: `cd frontend && npm install && npm run dev` → http://localhost:3001
  (API calls are passed to the backend).

## Roles

| Role | Can |
|---|---|
| Admin | everything, including users, company settings, audit log |
| Accountant | clients, products, services, invoices, payments, reports |
| Payment Recorder | sees clients and invoices; records payments with the slip |
| Payment Verifier | sees clients and invoices; verifies / rejects payments; reports |

## Rules

- Nothing is deleted: clients are archived, users disabled, products made
  inactive, services ended (end date); history stays.
- A client can be archived only after all its services have ended.
- Part-month charges: monthly amount × days used ÷ days in that month (the
  service form shows the first / last month's amount as you type).
- Once invoicing has billed part of a service, its start date and charge
  type are locked, and its end date can't go back before what was billed.

## Security

Read **SECURITY.md** before giving other people access: HTTPS, the secret
key, the limited database user (`scripts/db-setup.sql`), backups
(`scripts/backup.bat`) and two-step sign-in. In short, the app has:
cookie sessions that end on sign-out / 30 min idle / 12 h; two-step sign-in
with an authenticator app (admins can require it); strong-password rules;
sign-in lockout and logging; cross-site request protection; strict browser
security headers; and an audit log the database won't let anyone change.

## Reports

Reports (Admin, Accountant, Payment Verifier) — each has "Download Excel":

- **Ageing**: what each client owed on a chosen date, split into not yet
  due / 1–30 / 31–60 / 61–90 / over 90 days past the due date, plus opening
  balance and unapplied credit. Only verified payments dated on or before
  that day count.
- **Billed vs collected**: per month, issued invoices (by invoice date)
  against verified payments (by payment date), with tax and WHT.
- **MRC**: monthly recurring revenue being billed today, by product or by
  client, with each one's share.
- **Invoice register**: every issued / cancelled invoice in a period with
  client NTN / STRN, amount before tax, sales tax, total, paid, balance —
  for sales-tax returns. Totals leave out cancelled invoices.
- **Payment register**: payments in a period with WHT, who recorded and
  who verified.

The Excel files are real spreadsheets (numbers, dates, filters, frozen
header), made by the program itself — no extra software needed.

## Client statements

On a client's page, "Statement": pick the period, then print / save as PDF
or download Excel. It shows the balance brought forward, every issued
invoice and verified payment in the period (tax deducted on its own line),
a running balance, the closing balance (the same as the client's balance on
that day), the balance by age, and your bank details. Payments still waiting
for verification are mentioned but not counted.

## Payments

- A Payment Recorder records the payment with its slip (photo or PDF, max
  8 MB; not needed for cash) and applies it to the client's invoices
  ("Oldest first" fills it in). Whatever isn't applied is the client's
  credit, which can be applied to invoices later.
- "Tax deducted by client (WHT)": income tax the client withheld and paid
  for you. It counts as paid, alongside the amount received.
- A Payment Verifier checks it against the bank and verifies or rejects it.
  Only verified payments count. Whoever recorded a payment can't verify it
  (an Admin can do both).
- A verified payment can be reversed (bounced cheque, returned transfer):
  the invoices it paid become unpaid again.
- An invoice with payments on it can't be cancelled until they are rejected
  or reversed.
- Client balance = opening balance + issued invoices − verified payments.
- Slips are stored in the `uploads` folder (UPLOAD_DIR in .env). **Back it
  up together with the database.**

## Invoicing rules

- Monthly services are billed **in advance**: the run for December charges
  all of December. A part month (service starts or ends mid-month) is
  charged by days, and anything not billed yet (e.g. 18–30 Nov for a
  service that started on 18 Nov) is added to the next run as its own line.
- One-time charges go on the first run on or after their date; yearly
  charges a full year at a time from the start date.
- Suspended services are skipped; resuming sets "Don't bill before" to that
  day so the suspended time isn't billed. For services moved over from an
  older system, set "Don't bill before" to the first day to bill here.
- A charge already on a draft or issued invoice is never billed twice.
  Running the month again only adds what's new.
- Drafts can be edited (lines, dates, tax %, notes; a negative rate is a
  discount) or deleted. Issued invoices can't change; cancelling one keeps
  its number and makes its service charges billable again.
- Print / PDF: open the invoice, "Print / PDF", then "Save as PDF" in the
  print window. The logo, company details and bank details are those of the
  company that bills it (Company settings), copied when it is issued.

## Several companies
- Company settings lists your companies; "Add company" adds one. Each has
  its own logo (PNG / JPG / WEBP up to 2 MB, kept in `uploads/logos`), details,
  bank details, footer, invoice number prefix and counter (APX-000001,
  NET-000001…), and its own taxes (Taxes page). One is the default.
- Each service says which company bills it ("Billed by"; the default
  company unless changed). It can change only until the service is first
  on an invoice.
- The monthly run makes one draft per client **and** company, so a client
  with services from two companies gets two invoices. "Make monthly
  invoices" can also run for one company only.
- A manual invoice chooses its company; a draft with only typed-in lines can
  move to another company.
- Issuing copies the company's details and logo onto the invoice: renaming a
  company or changing its logo later doesn't change invoices already issued.
- Invoices, Services, Taxes and the MRC / Invoice register / Tax summary
  reports can be filtered by company. Payments, balances, ageing and
  statements are per client across all companies.
- The first start after this update makes your first company from the old
  company settings and puts every existing service, invoice and tax under it.
- Billing rules are checked by `go test ./internal/handlers/`.
- Every sign-in and change is in the audit log.
- 5 wrong passwords for an email or from an address → 15-minute pause.
- Disabling a user or changing their password ends their sessions at once.
