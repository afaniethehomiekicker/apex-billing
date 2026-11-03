package routes

import (
	"net/http"

	"apex-billing/internal/auth"
	"apex-billing/internal/handlers"

	"github.com/gin-gonic/gin"
)

// Register sets up every API route under /api.
func Register(r *gin.Engine) {
	api := r.Group("/api")
	api.Use(auth.SameOrigin())
	api.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	api.POST("/auth/login", handlers.Login)
	api.POST("/auth/mfa", handlers.LoginMFA)

	p := api.Group("")
	p.Use(auth.Required(), auth.Gate(handlers.Require2FA))
	{
		p.POST("/auth/logout", handlers.Logout)
		p.GET("/me", handlers.GetMe)
		p.PUT("/me/password", handlers.ChangeMyPassword)
		p.GET("/me/sessions", handlers.GetMySessions)
		p.DELETE("/me/sessions/:id", handlers.RevokeMySession)
		p.POST("/me/sessions/revoke-others", handlers.RevokeMyOtherSessions)
		p.POST("/me/2fa/setup", handlers.Setup2FA)
		p.POST("/me/2fa/enable", handlers.Enable2FA)
		p.POST("/me/2fa/disable", handlers.Disable2FA)
		p.POST("/me/2fa/recovery-codes", handlers.NewRecoveryCodes)
		p.PUT("/me/profile", handlers.UpdateMyProfile)
		p.PUT("/me/email", auth.Require("manage_users"), handlers.ChangeMyEmail)
		p.POST("/me/avatar", handlers.UploadMyAvatar)
		p.POST("/me/avatar/remove", handlers.RemoveMyAvatar)
		p.GET("/users/:id/avatar", handlers.GetUserAvatar)

		p.GET("/settings", handlers.GetSettings)
		p.PUT("/settings", auth.Require("manage_settings"), handlers.UpdateSettings)

		p.GET("/companies", auth.Require("view_billing"), handlers.GetCompanies)
		p.GET("/companies/:id", auth.Require("view_billing"), handlers.GetCompany)
		p.GET("/companies/:id/logo", auth.Require("view_billing"), handlers.GetCompanyLogo)
		p.POST("/companies", auth.Require("manage_settings"), handlers.CreateCompany)
		p.PUT("/companies/:id", auth.Require("manage_settings"), handlers.UpdateCompany)
		p.POST("/companies/:id/logo", auth.Require("manage_settings"), handlers.UploadCompanyLogo)
		p.POST("/companies/:id/logo/remove", auth.Require("manage_settings"), handlers.RemoveCompanyLogo)

		p.GET("/users", auth.Require("manage_users"), handlers.GetUsers)
		p.POST("/users", auth.Require("manage_users"), handlers.CreateUser)
		p.PUT("/users/:id", auth.Require("manage_users"), handlers.UpdateUser)
		p.POST("/users/:id/password", auth.Require("manage_users"), handlers.SetUserPassword)
		p.POST("/users/:id/2fa/reset", auth.Require("manage_users"), handlers.ResetUser2FA)
		p.POST("/users/:id/signout", auth.Require("manage_users"), handlers.SignOutUser)
		p.POST("/users/:id/avatar", auth.Require("manage_users"), handlers.UploadUserAvatar)
		p.POST("/users/:id/avatar/remove", auth.Require("manage_users"), handlers.RemoveUserAvatar)

		p.GET("/clients", auth.Require("view_billing"), handlers.GetClients)
		p.GET("/clients/:id", auth.Require("view_billing"), handlers.GetClient)
		p.GET("/clients/:id/statement", auth.Require("view_billing"), handlers.GetStatement)
		p.POST("/clients", auth.Require("manage_clients"), handlers.CreateClient)
		p.PUT("/clients/:id", auth.Require("manage_clients"), handlers.UpdateClient)

		p.GET("/products", auth.Require("view_billing"), handlers.GetProducts)
		p.GET("/products/:id", auth.Require("view_billing"), handlers.GetProduct)
		p.POST("/products", auth.Require("manage_billing"), handlers.CreateProduct)
		p.PUT("/products/:id", auth.Require("manage_billing"), handlers.UpdateProduct)

		p.GET("/subscriptions", auth.Require("view_billing"), handlers.GetSubscriptions)
		p.GET("/subscriptions/:id", auth.Require("view_billing"), handlers.GetSubscription)
		p.POST("/subscriptions", auth.Require("manage_billing"), handlers.CreateSubscription)
		p.PUT("/subscriptions/:id", auth.Require("manage_billing"), handlers.UpdateSubscription)

		p.GET("/taxes", auth.Require("view_billing"), handlers.GetTaxes)
		p.GET("/taxes/:id", auth.Require("view_billing"), handlers.GetTax)
		p.POST("/taxes", auth.Require("manage_billing"), handlers.CreateTax)
		p.PUT("/taxes/:id", auth.Require("manage_billing"), handlers.UpdateTax)
		p.POST("/products/:id/apply-taxes", auth.Require("manage_billing"), handlers.ApplyProductTaxes)

		p.GET("/sla-plans", auth.Require("view_billing"), handlers.GetSLAPlans)
		p.GET("/sla-plans/:id", auth.Require("view_billing"), handlers.GetSLAPlan)
		p.POST("/sla-plans", auth.Require("manage_billing"), handlers.CreateSLAPlan)
		p.PUT("/sla-plans/:id", auth.Require("manage_billing"), handlers.UpdateSLAPlan)

		p.GET("/contracts", auth.Require("view_billing"), handlers.GetContracts)
		p.GET("/contracts/:id", auth.Require("view_billing"), handlers.GetContract)
		p.GET("/contracts/:id/document", auth.Require("view_billing"), handlers.GetContractDocument)
		p.POST("/contracts", auth.Require("manage_billing"), handlers.CreateContract)
		p.PUT("/contracts/:id", auth.Require("manage_billing"), handlers.UpdateContract)
		p.POST("/contracts/:id/services", auth.Require("manage_billing"), handlers.LinkContractServices)
		p.POST("/contracts/:id/renew", auth.Require("manage_billing"), handlers.RenewContract)
		p.POST("/contracts/:id/terminate", auth.Require("manage_billing"), handlers.TerminateContract)
		p.POST("/contracts/:id/document", auth.Require("manage_billing"), handlers.UploadContractDocument)

		p.GET("/invoices", auth.Require("view_billing"), handlers.GetInvoices)
		p.GET("/invoices/:id", auth.Require("view_billing"), handlers.GetInvoice)
		p.GET("/invoices/:id/logo", auth.Require("view_billing"), handlers.GetInvoiceLogo)
		p.POST("/invoices", auth.Require("manage_billing"), handlers.CreateInvoice)
		p.POST("/invoices/generate", auth.Require("manage_billing"), handlers.GenerateInvoices)
		p.POST("/invoices/issue", auth.Require("manage_billing"), handlers.IssueInvoices)
		p.PUT("/invoices/:id", auth.Require("manage_billing"), handlers.UpdateInvoice)
		p.DELETE("/invoices/:id", auth.Require("manage_billing"), handlers.DeleteInvoice)
		p.POST("/invoices/:id/add-due", auth.Require("manage_billing"), handlers.AddDueCharges)
		p.POST("/invoices/:id/cancel", auth.Require("manage_billing"), handlers.CancelInvoice)

		p.GET("/payments", auth.Require("view_billing"), handlers.GetPayments)
		p.GET("/payments/open-invoices", auth.Require("view_billing"), handlers.OpenInvoices)
		p.GET("/payments/:id", auth.Require("view_billing"), handlers.GetPayment)
		p.GET("/payments/:id/slip", auth.Require("view_billing"), handlers.GetPaymentSlip)
		p.POST("/payments", auth.Require("record_payments"), handlers.RecordPayment)
		p.POST("/payments/:id/verify", auth.Require("verify_payments"), handlers.VerifyPayment)
		p.POST("/payments/:id/reject", auth.Require("verify_payments"), handlers.RejectPayment)
		p.POST("/payments/:id/reverse", auth.Require("verify_payments"), handlers.ReversePayment)
		p.POST("/payments/:id/allocate", auth.RequireAny("verify_payments", "manage_billing"), handlers.AllocatePayment)

		p.GET("/dashboard", auth.Require("view_billing"), handlers.GetDashboard)

		p.GET("/reports/ageing", auth.Require("view_reports"), handlers.ReportAgeing)
		p.GET("/reports/billed-collected", auth.Require("view_reports"), handlers.ReportBilledCollected)
		p.GET("/reports/mrc", auth.Require("view_reports"), handlers.ReportMRC)
		p.GET("/reports/invoices", auth.Require("view_reports"), handlers.ReportInvoices)
		p.GET("/reports/payments", auth.Require("view_reports"), handlers.ReportPayments)
		p.GET("/reports/taxes", auth.Require("view_reports"), handlers.ReportTaxes)

		p.GET("/audit", auth.Require("view_audit"), handlers.GetAuditLog)
	}
}
