package replication

import (
	"github.com/go-viper/mapstructure/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DatabaseCredentials struct", func() {
	Context("Decoding", func() {
		It("should successfully decode from string map", func() {
			input := map[string]string{
				"db.host":           "db-hostname",
				"db.port":           "1234",
				"db.user":           "db-user",
				"db.password":       "db-password",
				"db.admin_password": "db-admin-password",
				"db.admin_user":     "db-admin-user",
				"db.name":           "db-name",
			}

			var output DatabaseCredentials
			err := mapstructure.WeakDecode(input, &output)

			Expect(err).ToNot(HaveOccurred())
			Expect(output.Host).To(Equal("db-hostname"))
			Expect(output.Port).To(Equal("1234"))
			Expect(output.User).To(Equal("db-user"))
			Expect(output.Password).To(Equal("db-password"))
			Expect(output.AdminPassword).To(Equal("db-admin-password"))
			Expect(output.AdminUser).To(Equal("db-admin-user"))
			Expect(output.DatabaseName).To(Equal("db-name"))
		})

		It("should successfully decode from bytearray map", func() {
			input := map[string][]byte{
				"db.host":           []byte("db-hostname"),
				"db.port":           []byte("1234"),
				"db.user":           []byte("db-user"),
				"db.password":       []byte("db-password"),
				"db.admin_password": []byte("db-admin-password"),
				"db.admin_user":     []byte("db-admin-user"),
				"db.name":           []byte("db-name"),
			}

			var output DatabaseCredentials
			err := mapstructure.WeakDecode(input, &output)

			Expect(err).ToNot(HaveOccurred())
			Expect(output.Host).To(Equal("db-hostname"))
			Expect(output.Port).To(Equal("1234"))
			Expect(output.User).To(Equal("db-user"))
			Expect(output.Password).To(Equal("db-password"))
			Expect(output.AdminPassword).To(Equal("db-admin-password"))
			Expect(output.AdminUser).To(Equal("db-admin-user"))
			Expect(output.DatabaseName).To(Equal("db-name"))
		})
	})
})
