package replication

type DatabaseCredentials struct {
	// Hostname
	Host string `mapstructure:"db.host"`

	// Port
	Port string `mapstructure:"db.port"`

	// Regular username
	User string `mapstructure:"db.user"`

	// Password for the regular user
	Password string `mapstructure:"db.password"`

	// Name of the database
	DatabaseName string `mapstructure:"db.name"`

	// Username of the admin account
	AdminUser string `mapstructure:"db.admin_user"`

	// Password of the admin account
	AdminPassword string `mapstructure:"db.admin_password"`
}
