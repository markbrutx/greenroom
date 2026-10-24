create table retros (
  id {{ID}},
  created_at bigint not null,
  title text not null default '',
  warmup text not null,
  cols text not null,
  hidden {{BOOL}} not null default {{FALSE}},
  timer_end bigint not null default 0
);

create table cards (
  id {{ID}},
  retro_id bigint not null references retros(id) on delete cascade,
  col text not null,
  author text not null,
  body text not null default '',
  owner text not null default '',
  done {{BOOL}} not null default {{FALSE}},
  done_retro bigint,
  done_at bigint,
  deleted {{BOOL}} not null default {{FALSE}},
  ver bigint not null default 0,
  created_at bigint not null,
  updated_at bigint not null
);

create index cards_retro on cards (retro_id) where not deleted;

create index cards_open_actions on cards (retro_id) where col = 'a' and not deleted and not done;

create table reactions (
  card_id bigint not null references cards(id) on delete cascade,
  who text not null,
  emoji text not null,
  created_at bigint not null,
  primary key (card_id, who, emoji)
);
