CREATE SCHEMA IF NOT EXISTS blas;
CREATE TABLE IF NOT EXISTS blas.durable_keywords(
 run_id text NOT NULL,kid bigint NOT NULL,keyword text COLLATE "C" NOT NULL,
 PRIMARY KEY(run_id,kid),UNIQUE(run_id,keyword));
-- Each object is independently addressable. Only current query objects are read.
CREATE TABLE IF NOT EXISTS blas.durable_objects(
 run_id text NOT NULL,kind text NOT NULL,owner bigint NOT NULL,oid bigint NOT NULL,
 object bytea NOT NULL,keywords jsonb NOT NULL,lids jsonb NOT NULL,
 ts_min bigint NOT NULL,ts_max bigint NOT NULL,
 PRIMARY KEY(run_id,kind,owner,oid));
CREATE TABLE IF NOT EXISTS blas.durable_files(
 run_id text NOT NULL,fid bigint NOT NULL,block_height bigint NOT NULL,
 entries bigint NOT NULL,metadata bytea NOT NULL,
 PRIMARY KEY(run_id,fid));
CREATE TABLE IF NOT EXISTS blas.durable_headers(
 run_id text NOT NULL,height bigint NOT NULL,segment bigint NOT NULL,
 payload bytea NOT NULL,PRIMARY KEY(run_id,height));
CREATE INDEX IF NOT EXISTS blas_durable_segment ON blas.durable_headers(run_id,segment,height);
