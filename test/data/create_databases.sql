CREATE DATABASE publisher_db;
CREATE USER publisher_user WITH PASSWORD 'publisher_password';
GRANT ALL PRIVILEGES ON DATABASE publisher_db TO publisher_user;
ALTER ROLE publisher_user WITH REPLICATION;

CREATE DATABASE subscriber_db;
CREATE USER subscriber_user WITH PASSWORD 'subscriber_password';
GRANT ALL PRIVILEGES ON DATABASE subscriber_db TO subscriber_user;
GRANT pg_create_subscription TO subscriber_user;

\c publisher_db publisher_user
CREATE SCHEMA IF NOT EXISTS published_data;
CREATE TABLE IF NOT EXISTS published_data.people
  (id UUID PRIMARY KEY, name VARCHAR(255), email VARCHAR(255) UNIQUE, birthyear INT);
INSERT INTO published_data.people VALUES
  (gen_random_uuid(), 'My Name', 'My Email', 9999),
  (gen_random_uuid(), 'Your Name', 'Your Email', 1111)
  ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS published_data.cities
  (id UUID PRIMARY KEY, name VARCHAR(255) UNIQUE, zip VARCHAR(255), country VARCHAR(255));
INSERT INTO published_data.cities VALUES
  (gen_random_uuid(), 'New York', '900 22', 'USA'),
  (gen_random_uuid(), 'Rio', '111 88', 'Brazil'),
  (gen_random_uuid(), 'Tokyo', '91378', 'Japan')
  ON CONFLICT DO NOTHING;

CREATE PUBLICATION publication_v1;
ALTER PUBLICATION publication_v1 ADD TABLE published_data.people (id, name);
ALTER PUBLICATION publication_v1 ADD TABLE published_data.cities (id, name, zip, country);

\c subscriber_db postgres
GRANT SELECT ON pg_subscription TO subscriber_user;

\c subscriber_db subscriber_user
CREATE SCHEMA IF NOT EXISTS published_data;
CREATE TABLE IF NOT EXISTS published_data.people
  (id UUID PRIMARY KEY, name VARCHAR(255));
CREATE TABLE IF NOT EXISTS published_data.cities
  (id UUID PRIMARY KEY, name VARCHAR(255) UNIQUE, zip VARCHAR(255), country VARCHAR(255));

-- when running publisher and subscriber on the same cluster slot has to be created manually
CREATE SUBSCRIPTION publication_v1
  CONNECTION 'password=null'
  PUBLICATION publication_v1
  WITH (connect=false);

\c publisher_db publisher_user
SELECT pg_create_logical_replication_slot('publication_v1', 'pgoutput');
