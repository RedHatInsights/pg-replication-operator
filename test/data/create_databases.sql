CREATE DATABASE publisher_db;
CREATE USER publisher_user WITH PASSWORD 'publisher_password';
GRANT ALL PRIVILEGES ON DATABASE publisher_db TO publisher_user;

CREATE DATABASE subscriber_db;
CREATE USER subscriber_user WITH PASSWORD 'subscriber_password';
GRANT ALL PRIVILEGES ON DATABASE subscriber_db TO subscriber_user;
