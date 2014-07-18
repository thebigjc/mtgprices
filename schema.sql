CREATE TABLE card_prices (name text, set_name text, buy int, sell int, stock int, ts text);
CREATE UNIQUE INDEX card_names_idx on card_prices(name, set_name, ts);
ALTER TABLE card_prices ADD COLUMN clean int null;

