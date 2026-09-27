-- TPCH-lite deterministic seed for Snowflake.
-- Produces fixtures matching db/seed.sql (Postgres) and db/seed_duckdb.sql
-- (DuckDB) so the differential oracle can use Snowflake as the fidelity
-- verification target.
--
-- Snowflake notes vs the other seeds:
--   * no generate_series: use TABLE(GENERATOR(ROWCOUNT => n)) with seq4()+1
--   * '/' on integers yields a NUMBER: use FLOOR for integer division
--   * date arithmetic via DATEADD(day, n, 'yyyy-mm-dd')

CREATE OR REPLACE TABLE region (
    r_regionkey INTEGER,
    r_name      VARCHAR(25),
    r_comment   VARCHAR(152)
);

CREATE OR REPLACE TABLE nation (
    n_nationkey INTEGER,
    n_name      VARCHAR(25),
    n_regionkey INTEGER,
    n_comment   VARCHAR(152)
);

CREATE OR REPLACE TABLE customer (
    c_custkey    INTEGER,
    c_name       VARCHAR(25),
    c_address    VARCHAR(40),
    c_nationkey  INTEGER,
    c_phone      VARCHAR(15),
    c_acctbal    DECIMAL(15,2),
    c_mktsegment VARCHAR(10),
    c_comment    VARCHAR(117)
);

CREATE OR REPLACE TABLE orders (
    o_orderkey      INTEGER,
    o_custkey       INTEGER,
    o_orderstatus   VARCHAR(1),
    o_totalprice    DECIMAL(15,2),
    o_orderdate     DATE,
    o_orderpriority VARCHAR(15),
    o_clerk         VARCHAR(15),
    o_shippriority  INTEGER,
    o_comment       VARCHAR(79)
);

CREATE OR REPLACE TABLE lineitem (
    l_orderkey      INTEGER,
    l_partkey       INTEGER,
    l_suppkey       INTEGER,
    l_linenumber    INTEGER,
    l_quantity      DECIMAL(15,2),
    l_extendedprice DECIMAL(15,2),
    l_discount      DECIMAL(15,2),
    l_tax           DECIMAL(15,2),
    l_returnflag    VARCHAR(1),
    l_linestatus    VARCHAR(1),
    l_shipdate      DATE,
    l_commitdate    DATE,
    l_receiptdate   DATE,
    l_shipinstruct  VARCHAR(25),
    l_shipmode      VARCHAR(10),
    l_comment       VARCHAR(44)
);

INSERT INTO region (r_regionkey, r_name, r_comment)
SELECT i,
       CASE i
           WHEN 1 THEN 'AFRICA'
           WHEN 2 THEN 'AMERICA'
           WHEN 3 THEN 'ASIA'
           WHEN 4 THEN 'EUROPE'
           ELSE 'MIDDLE EAST'
       END,
       'region comment ' || i
FROM (SELECT seq4() + 1 AS i FROM TABLE(GENERATOR(ROWCOUNT => 5)));

INSERT INTO nation (n_nationkey, n_name, n_regionkey, n_comment)
SELECT i,
       'NATION_' || i,
       FLOOR((i - 1) / 5) + 1,
       'nation comment ' || i
FROM (SELECT seq4() + 1 AS i FROM TABLE(GENERATOR(ROWCOUNT => 25)));

INSERT INTO customer (c_custkey, c_name, c_address, c_nationkey, c_phone, c_acctbal, c_mktsegment, c_comment)
SELECT i,
       'Customer#' || i,
       'Address ' || i,
       ((i - 1) % 25) + 1,
       '123-456-' || i,
       CAST(((i * 3717) % 100000) AS DECIMAL(15,2)) / 100,
       CASE (i % 5)
           WHEN 0 THEN 'AUTOMOBILE'
           WHEN 1 THEN 'BUILDING'
           WHEN 2 THEN 'FURNITURE'
           WHEN 3 THEN 'MACHINERY'
           ELSE 'HOUSEHOLD'
       END,
       'customer comment ' || i
FROM (SELECT seq4() + 1 AS i FROM TABLE(GENERATOR(ROWCOUNT => 200)));

INSERT INTO orders (o_orderkey, o_custkey, o_orderstatus, o_totalprice, o_orderdate, o_orderpriority, o_clerk, o_shippriority, o_comment)
SELECT i,
       ((i - 1) % 200) + 1,
       CASE (i % 3)
           WHEN 0 THEN 'F'
           WHEN 1 THEN 'O'
           ELSE 'P'
       END,
       CAST(((i * 9123) % 1000000) AS DECIMAL(15,2)) / 100,
       DATEADD(day, (i * 7) % 2190, '1992-01-01'),
       CASE (i % 5)
           WHEN 0 THEN '1-URGENT'
           WHEN 1 THEN '2-HIGH'
           WHEN 2 THEN '3-MEDIUM'
           WHEN 3 THEN '4-NOT SPECIFIED'
           ELSE '5-LOW'
       END,
       'Clerk#' || i,
       i % 3,
       'orders comment ' || i
FROM (SELECT seq4() + 1 AS i FROM TABLE(GENERATOR(ROWCOUNT => 200)));

INSERT INTO lineitem (l_orderkey, l_partkey, l_suppkey, l_linenumber, l_quantity, l_extendedprice, l_discount, l_tax, l_returnflag, l_linestatus, l_shipdate, l_commitdate, l_receiptdate, l_shipinstruct, l_shipmode, l_comment)
SELECT FLOOR((i - 1) / 4) + 1,
       ((i * 31) % 2000) + 1,
       ((i * 17) % 100) + 1,
       ((i - 1) % 4) + 1,
       CAST(((i % 50) + 1) AS DECIMAL(15,2)),
       CAST(((i * 977) % 100000) AS DECIMAL(15,2)) / 100,
       CAST(((i * 11) % 10) AS DECIMAL(15,2)) / 100,
       CAST(((i * 7) % 8) AS DECIMAL(15,2)) / 100,
       CASE (i % 3)
           WHEN 0 THEN 'R'
           WHEN 1 THEN 'A'
           ELSE 'N'
       END,
       CASE (i % 2)
           WHEN 0 THEN 'F'
           ELSE 'O'
       END,
       DATEADD(day, (i * 13) % 2000, '1992-01-01'),
       DATEADD(day, (i * 17) % 1900, '1992-02-01'),
       DATEADD(day, (i * 19) % 1800, '1992-03-01'),
       CASE (i % 4)
           WHEN 0 THEN 'DELIVER IN PERSON'
           WHEN 1 THEN 'COLLECT COD'
           WHEN 2 THEN 'TAKE BACK RETURN'
           ELSE 'NONE'
       END,
       CASE (i % 5)
           WHEN 0 THEN 'RAIL'
           WHEN 1 THEN 'TRUCK'
           WHEN 2 THEN 'AIR'
           WHEN 3 THEN 'SHIP'
           ELSE 'MAIL'
       END,
       'lineitem comment ' || i
FROM (SELECT seq4() + 1 AS i FROM TABLE(GENERATOR(ROWCOUNT => 200)));
