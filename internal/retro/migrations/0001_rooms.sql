create table rooms (
  id integer primary key autoincrement,
  code text not null unique,
  host_secret text not null,
  open integer not null default 0,
  warmup text not null,
  cols text not null,
  created_at integer not null,
  active_at integer not null
);

create index rooms_active on rooms (active_at);

create table members (
  id integer primary key autoincrement,
  room_id integer not null references rooms(id) on delete cascade,
  name text not null,
  name_key text not null,
  host integer not null default 0,
  status text not null,
  created_at integer not null,
  seen_at integer not null
);

create index members_room on members (room_id, status);

create unique index members_name on members (room_id, name_key) where status in ('pending', 'admitted');

create table cards (
  id integer primary key autoincrement,
  room_id integer not null references rooms(id) on delete cascade,
  col text not null,
  author text not null,
  body text not null default '',
  owner text not null default '',
  ver integer not null default 0,
  created_at integer not null,
  updated_at integer not null
);

create index cards_room on cards (room_id);

create table reactions (
  card_id integer not null references cards(id) on delete cascade,
  who text not null,
  emoji text not null,
  created_at integer not null,
  primary key (card_id, who, emoji)
);
