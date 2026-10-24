create table people (
  key text primary key,
  name text not null,
  last_seen bigint not null
);
create index people_seen on people (last_seen);
