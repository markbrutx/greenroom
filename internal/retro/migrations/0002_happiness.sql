alter table rooms add column mood_shown integer not null default 0;

create table moods (
  member_id integer primary key references members(id) on delete cascade,
  room_id integer not null references rooms(id) on delete cascade,
  score integer not null check (score between 1 and 5),
  created_at integer not null
);

create index moods_room on moods (room_id);
