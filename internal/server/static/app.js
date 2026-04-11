'use strict';

// Configuration injected by the HTML template.
// window.SLOPASK = { slug, token, basePath, isAdmin }

(function() {

var CFG = window.SLOPASK || {};
var basePath = CFG.basePath || '';
var isAdmin = CFG.isAdmin || false;

// Voter ID: random UUID stored in localStorage.
function getVoterID() {
  var id = localStorage.getItem('slopask_voter_id');
  if (!id) {
    id = crypto.randomUUID();
    localStorage.setItem('slopask_voter_id', id);
  }
  return id;
}
var voterID = getVoterID();

// Voted question IDs persisted in localStorage.
function loadVotedSet() {
  try {
    var raw = localStorage.getItem('slopask_votes');
    return raw ? new Set(JSON.parse(raw)) : new Set();
  } catch (_) {
    return new Set();
  }
}
function saveVotedSet() {
  localStorage.setItem('slopask_votes', JSON.stringify([...votedSet]));
}
var votedSet = loadVotedSet();

// Own questions: IDs of questions this user posted (for self-delete).
function loadMyQuestions() {
  try {
    var raw = localStorage.getItem('slopask_my_questions');
    return raw ? new Set(JSON.parse(raw)) : new Set();
  } catch (_) { return new Set(); }
}
function saveMyQuestions() {
  localStorage.setItem('slopask_my_questions', JSON.stringify([...myQuestions]));
}
var myQuestions = loadMyQuestions();

// Answer votes persisted in localStorage: { answerID: direction }
function loadAnswerVotes() {
  try {
    var raw = localStorage.getItem('slopask_answer_votes');
    return raw ? JSON.parse(raw) : {};
  } catch (_) {
    return {};
  }
}
function saveAnswerVotes() {
  localStorage.setItem('slopask_answer_votes', JSON.stringify(answerVotes));
}
var answerVotes = loadAnswerVotes();

// Track which version is currently displayed per question.
var answerVersionIndex = {};

var questions = [];
var sortMode = 'votes';

// Relative time formatting.
function relTime(unixSeconds) {
  var ms = Date.now() - unixSeconds * 1000;
  var m = Math.floor(ms / 60000);
  if (m < 1) return 'now';
  if (m < 60) return m + 'm';
  var h = Math.floor(m / 60);
  if (h < 24) return h + 'h';
  return Math.floor(h / 24) + 'd';
}

function esc(s) {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

// Minimal markdown renderer.
function renderMd(text) {
  var html = esc(text);
  // Fenced code blocks.
  html = html.replace(/```(\w*)\n([\s\S]*?)```/g, function(_, lang, code) {
    return '<pre><code' + (lang ? ' class="language-' + lang + '"' : '') + '>' + code.replace(/\n$/, '') + '</code></pre>';
  });
  // Line-based blocks.
  var lines = html.split('\n');
  var out = [], inList = false, listType = '';
  for (var i = 0; i < lines.length; i++) {
    var line = lines[i];
    if (line.includes('<pre>')) {
      out.push(line);
      while (i < lines.length - 1 && !lines[i].includes('</pre>')) { i++; out.push(lines[i]); }
      continue;
    }
    var hm = line.match(/^(#{1,4})\s+(.+)$/);
    if (hm) {
      if (inList) { out.push(listType === 'ul' ? '</ul>' : '</ol>'); inList = false; }
      out.push('<h' + hm[1].length + '>' + hm[2] + '</h' + hm[1].length + '>');
      continue;
    }
    if (/^(-{3,}|\*{3,}|_{3,})$/.test(line.trim())) {
      if (inList) { out.push(listType === 'ul' ? '</ul>' : '</ol>'); inList = false; }
      out.push('<hr>');
      continue;
    }
    if (line.startsWith('&gt; ')) {
      if (inList) { out.push(listType === 'ul' ? '</ul>' : '</ol>'); inList = false; }
      out.push('<blockquote>' + line.slice(5) + '</blockquote>');
      continue;
    }
    var ulm = line.match(/^[-*+]\s+(.+)$/);
    if (ulm) {
      if (!inList || listType !== 'ul') { if (inList) out.push('</ol>'); out.push('<ul>'); inList = true; listType = 'ul'; }
      out.push('<li>' + ulm[1] + '</li>');
      continue;
    }
    var olm = line.match(/^\d+\.\s+(.+)$/);
    if (olm) {
      if (!inList || listType !== 'ol') { if (inList) out.push('</ul>'); out.push('<ol>'); inList = true; listType = 'ol'; }
      out.push('<li>' + olm[1] + '</li>');
      continue;
    }
    if (inList) { out.push(listType === 'ul' ? '</ul>' : '</ol>'); inList = false; }
    out.push(line);
  }
  if (inList) out.push(listType === 'ul' ? '</ul>' : '</ol>');
  html = out.join('\n');
  // Inline formatting.
  html = html
    .replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
    .replace(/\*(.+?)\*/g, '<em>$1</em>')
    .replace(/~~(.+?)~~/g, '<del>$1</del>')
    .replace(/`(.+?)`/g, '<code>$1</code>')
    .replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>')
    .replace(/(^|[\s(])((https?:\/\/)[^\s<)]+)/gm, '$1<a href="$2" target="_blank" rel="noopener">$2</a>');
  return html;
}

var ARROW_UP = '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M3 10l5-5 5 5"/></svg>';
var ARROW_LEFT = '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M10 3l-5 5 5 5"/></svg>';
var ARROW_RIGHT = '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M6 3l5 5-5 5"/></svg>';
var THUMB_UP = '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.2"><path d="M5 14H3a1 1 0 01-1-1V8a1 1 0 011-1h2m0 7V7m0 7h5.5a1.5 1.5 0 001.45-1.1l1.2-4.8A1.5 1.5 0 0013.7 6H10V3.5A1.5 1.5 0 008.5 2L5 7"/></svg>';
var THUMB_DOWN = '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.2"><path d="M11 2h2a1 1 0 011 1v5a1 1 0 01-1 1h-2m0-7v7m0-7H5.5A1.5 1.5 0 004.05 3.1l-1.2 4.8A1.5 1.5 0 004.3 10H8v2.5A1.5 1.5 0 009.5 14L13 9"/></svg>';

function renderMediaHTML(mediaList, parentType, canDelete) {
  if (!mediaList || mediaList.length === 0) return '';
  var html = '<div class="q-media">';
  for (var i = 0; i < mediaList.length; i++) {
    var m = mediaList[i];
    html += '<div class="media-item">';
    if (m.kind === 'image') {
      html += '<img src="' + esc(m.url) + '" alt="' + esc(m.filename) + '">';
    } else if (m.kind === 'audio') {
      html += '<audio controls preload="metadata" src="' + esc(m.url) + '"></audio>';
    } else if (m.kind === 'video') {
      html += '<video controls preload="metadata" src="' + esc(m.url) + '"></video>';
    }
    if (canDelete) {
      var pt = parentType || 'question';
      html += '<button class="media-delete" data-mid="' + m.id + '" data-type="' + pt + '">x</button>';
    }
    html += '</div>';
  }
  html += '</div>';
  return html;
}

function renderAnswerHTML(q) {
  var answers = q.answers;
  if (!answers || answers.length === 0) return '';

  // Determine which version to show.
  var idx = answerVersionIndex[q.id];
  if (idx === undefined || idx >= answers.length) {
    idx = answers.length - 1;
    answerVersionIndex[q.id] = idx;
  }
  var answer = answers[idx];
  var total = answers.length;
  var current = idx + 1;

  var html = '<div class="q-answer" data-answer-id="' + answer.id + '">';
  html += '<div class="q-answer-text">' + renderMd(answer.body) + '</div>';
  html += renderMediaHTML(answer.media, 'answer', isAdmin);

  // Version navigation + thumbs bar.
  var myVote = answerVotes[answer.id] || 0;
  html += '<div class="q-answer-bar">';
  if (total > 1) {
    var prevDisabled = idx === 0 ? ' disabled' : '';
    var nextDisabled = idx === total - 1 ? ' disabled' : '';
    html += '<button class="answer-nav-btn answer-prev" data-qid="' + q.id + '"' + prevDisabled + '>' + ARROW_LEFT + '</button>';
    html += '<span class="answer-version">v' + current + '/' + total + '</span>';
    html += '<button class="answer-nav-btn answer-next" data-qid="' + q.id + '"' + nextDisabled + '>' + ARROW_RIGHT + '</button>';
  } else {
    html += '<span class="answer-version">v1</span>';
  }
  html += '<span class="answer-spacer"></span>';
  html += '<span class="answer-thumb answer-thumb-up' + (myVote === 1 ? ' voted' : '') + '" data-aid="' + answer.id + '" data-dir="1">' +
    THUMB_UP + '<span class="thumb-count">' + (answer.thumbs_up || 0) + '</span></span>';
  html += '<span class="answer-thumb answer-thumb-down' + (myVote === -1 ? ' voted' : '') + '" data-aid="' + answer.id + '" data-dir="-1">' +
    THUMB_DOWN + '<span class="thumb-count">' + (answer.thumbs_down || 0) + '</span></span>';
  html += '</div>';
  html += '</div>';
  return html;
}

function renderQuestions() {
  var sorted = questions.slice().sort(function(a, b) {
    if (sortMode === 'votes') return b.vote_count - a.vote_count || b.created_at - a.created_at;
    return b.created_at - a.created_at;
  });
  var c = document.getElementById('questions');
  c.innerHTML = sorted.map(function(q) {
    var canDeleteMedia = isAdmin || myQuestions.has(q.id);
    var media = renderMediaHTML(q.media, 'question', canDeleteMedia);
    var ans = renderAnswerHTML(q);
    var v = votedSet.has(q.id);

    var adminBtns = '';
    if (isAdmin) {
      adminBtns += '<button class="q-delete" data-id="' + q.id + '">delete</button>';
    } else if (myQuestions.has(q.id)) {
      adminBtns += '<button class="q-delete q-self-delete" data-id="' + q.id + '">delete</button>';
    }

    var answerForm = '';
    if (isAdmin) {
      var hasAnswers = q.answers && q.answers.length > 0;
      var label = hasAnswers ? 'new version' : 'answer';
      answerForm = '<div class="q-answer-form" data-qid="' + q.id + '">' +
        '<textarea placeholder="' + label + '..."></textarea>' +
        '<div class="answer-file-list" style="font-size:0.75rem;color:#555"></div>' +
        '<div class="answer-bar">' +
          '<label class="att-btn" title="image" style="width:32px;height:32px">' +
            '<svg viewBox="0 0 16 16" fill="none" stroke="#000" stroke-width="1.2"><rect x="1.5" y="2.5" width="13" height="11" rx="1"/><circle cx="5.5" cy="6.5" r="1.5"/><path d="M1.5 11l3.5-3.5 2.5 2.5 2.5-3 4 4"/></svg>' +
            '<input type="file" accept="image/*" hidden class="answer-file">' +
          '</label>' +
          '<button type="button" class="att-btn answer-rec-audio" title="record audio" style="width:32px;height:32px">' +
            '<svg viewBox="0 0 16 16" fill="none" stroke="#000" stroke-width="1.2"><rect x="5.5" y="1.5" width="5" height="8" rx="2.5"/><path d="M3 8.5a5 5 0 0010 0"/><line x1="8" y1="13.5" x2="8" y2="15"/></svg>' +
          '</button>' +
          '<button type="button" class="att-btn answer-rec-video" title="record video" style="width:32px;height:32px">' +
            '<svg viewBox="0 0 16 16" fill="none" stroke="#000" stroke-width="1.2"><rect x="1.5" y="3.5" width="9" height="9" rx="1"/><path d="M10.5 6l4-2.5v9L10.5 10"/></svg>' +
          '</button>' +
          '<label class="att-btn" title="file" style="width:32px;height:32px">' +
            '<svg viewBox="0 0 16 16" fill="none" stroke="#000" stroke-width="1.2"><path d="M13.5 7l-5.5 5.5a3 3 0 01-4.24-4.24l6.5-6.5a2 2 0 012.83 2.83l-6.5 6.5a1 1 0 01-1.42-1.42L10.5 4.5"/></svg>' +
            '<input type="file" hidden class="answer-file">' +
          '</label>' +
          '<div class="spacer"></div>' +
          '<button class="answer-submit-btn">' + label + '</button>' +
        '</div>' +
      '</div>';
    }

    return '<div class="q" data-qid="' + q.id + '"><div class="q-body">' + renderMd(q.body) + '</div>' + media +
      '<div class="q-meta">' +
        '<span class="q-vote' + (v ? ' voted' : '') + '" data-id="' + q.id + '">' +
          ARROW_UP + '<span class="vote-count">' + q.vote_count + '</span></span>' +
        '<span>' + relTime(q.created_at) + '</span>' +
        adminBtns +
      '</div>' + ans + answerForm + '</div>';
  }).join('');
  if (typeof renderMathInElement === 'function') {
    renderMathInElement(c, {
      delimiters: [{ left: '$$', right: '$$', display: true }, { left: '$', right: '$', display: false }],
      throwOnError: false
    });
  }
  // Collapse long questions (>3 lines of text) except the expanded one.
  var items = c.querySelectorAll('.q');
  for (var i = 0; i < items.length; i++) {
    var qid = parseInt(items[i].dataset.qid);
    var body = items[i].querySelector('.q-body');
    var isLong = body && body.scrollHeight > 60;
    if (qid !== expandedQID && isLong) items[i].classList.add('collapsed');
  }
}

var expandedQID = null;

// Accordion: click to expand, collapse others.
document.getElementById('questions').addEventListener('click', function(e) {
  // Don't toggle when clicking interactive elements.
  if (e.target.closest('button, a, .q-vote, .q-answer-form, textarea, input, label, video, audio, .answer-thumb, .answer-nav-btn')) return;
  var qEl = e.target.closest('.q');
  if (!qEl) return;
  var qid = parseInt(qEl.dataset.qid);
  if (expandedQID === qid) {
    expandedQID = null;
  } else {
    expandedQID = qid;
  }
  var items = document.querySelectorAll('#questions .q');
  for (var i = 0; i < items.length; i++) {
    var id = parseInt(items[i].dataset.qid);
    items[i].classList.toggle('collapsed', id !== expandedQID);
  }
});

// Fetch questions from the server.
function fetchQuestions() {
  fetch(basePath + '/questions?sort=' + sortMode)
    .then(function(r) { return r.json(); })
    .then(function(data) {
      questions = data || [];
      renderQuestions();
    })
    .catch(function(err) { console.error('fetch questions:', err); });
}

// Voting on questions.
document.getElementById('questions').addEventListener('click', function(e) {
  var el = e.target.closest('.q-vote');
  if (el) {
    var id = parseInt(el.dataset.id);
    if (votedSet.has(id)) {
      fetch(basePath + '/questions/' + id + '/vote', {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ voter_id: voterID })
      }).then(function(r) { return r.json(); }).then(function(data) {
        votedSet.delete(id);
        saveVotedSet();
        var q = questions.find(function(q) { return q.id === id; });
        if (q) q.vote_count = data.vote_count;
        renderQuestions();
      });
    } else {
      fetch(basePath + '/questions/' + id + '/vote', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ voter_id: voterID })
      }).then(function(r) { return r.json(); }).then(function(data) {
        votedSet.add(id);
        saveVotedSet();
        var q = questions.find(function(q) { return q.id === id; });
        if (q) q.vote_count = data.vote_count;
        renderQuestions();
      });
    }
    return;
  }

  // Delete question (admin or own).
  var del = e.target.closest('.q-delete');
  if (del) {
    var qid = parseInt(del.dataset.id);
    if (!confirm('Delete?')) return;
    var opts = { method: 'DELETE' };
    if (!isAdmin) {
      opts.headers = { 'Content-Type': 'application/json' };
      opts.body = JSON.stringify({ voter_id: voterID });
    }
    fetch(basePath + '/questions/' + qid, opts).then(function(r) {
      if (r.ok) {
        questions = questions.filter(function(q) { return q.id !== qid; });
        myQuestions.delete(qid);
        saveMyQuestions();
        renderQuestions();
      }
    });
  }

  // Delete individual media (admin or own question).
  var mediaDel = e.target.closest('.media-delete');
  if (mediaDel) {
    var mid = parseInt(mediaDel.dataset.mid);
    var mtype = mediaDel.dataset.type;
    var delUrl = basePath + (isAdmin ? '/media/' + mtype + '/' + mid : '/questions/' + mediaDel.closest('.q').dataset.qid + '/media/' + mid + '?voter_id=' + voterID);
    fetch(delUrl, { method: 'DELETE' }).then(function(r) {
      if (r.ok) fetchQuestions();
    });
    return;
  }

  // Answer version navigation.
  var prevBtn = e.target.closest('.answer-prev');
  if (prevBtn) {
    var qid = parseInt(prevBtn.dataset.qid);
    if (answerVersionIndex[qid] > 0) {
      answerVersionIndex[qid]--;
      renderQuestions();
    }
    return;
  }
  var nextBtn = e.target.closest('.answer-next');
  if (nextBtn) {
    var qid = parseInt(nextBtn.dataset.qid);
    var q = questions.find(function(q) { return q.id === qid; });
    if (q && answerVersionIndex[qid] < q.answers.length - 1) {
      answerVersionIndex[qid]++;
      renderQuestions();
    }
    return;
  }

  // Thumbs voting on answers.
  var thumb = e.target.closest('.answer-thumb');
  if (thumb) {
    var aid = parseInt(thumb.dataset.aid);
    var dir = parseInt(thumb.dataset.dir);
    var currentVote = answerVotes[aid] || 0;
    var newDir = (currentVote === dir) ? 0 : dir;

    fetch(basePath + '/answers/' + aid + '/vote', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ voter_id: voterID, direction: newDir })
    }).then(function(r) { return r.json(); }).then(function(data) {
      if (newDir === 0) {
        delete answerVotes[aid];
      } else {
        answerVotes[aid] = newDir;
      }
      saveAnswerVotes();
      // Update the answer's thumbs counts in memory.
      for (var i = 0; i < questions.length; i++) {
        var answers = questions[i].answers;
        if (!answers) continue;
        for (var j = 0; j < answers.length; j++) {
          if (answers[j].id === data.answer_id) {
            answers[j].thumbs_up = data.thumbs_up;
            answers[j].thumbs_down = data.thumbs_down;
          }
        }
      }
      renderQuestions();
    });
    return;
  }
});

// Admin: per-form pending files for answer recordings.
var answerPendingFiles = {};

// Admin: answer file inputs.
document.getElementById('questions').addEventListener('change', function(e) {
  var fi = e.target.closest('.answer-file');
  if (!fi) return;
  var form = fi.closest('.q-answer-form');
  var qid = form.dataset.qid;
  if (!answerPendingFiles[qid]) answerPendingFiles[qid] = [];
  for (var i = 0; i < fi.files.length; i++) {
    answerPendingFiles[qid].push(fi.files[i]);
  }
  var fl = form.querySelector('.answer-file-list');
  if (fl) fl.innerHTML += '<span>' + fi.files[0].name + '</span> ';
});

// Admin: answer audio/video recording.
document.getElementById('questions').addEventListener('click', function(e) {
  var audioBtn = e.target.closest('.answer-rec-audio');
  var videoBtn = e.target.closest('.answer-rec-video');
  if (!audioBtn && !videoBtn) return;
  var mode = audioBtn ? 'audio' : 'video';
  var form = (audioBtn || videoBtn).closest('.q-answer-form');
  var qid = form.dataset.qid;
  var constraints = mode === 'video' ? { audio: true, video: true } : { audio: true };
  navigator.mediaDevices.getUserMedia(constraints).then(function(stream) {
    var chunks = [];
    var mr = new MediaRecorder(stream, { mimeType: mode === 'video' ? 'video/webm' : 'audio/webm' });
    mr.ondataavailable = function(e) { if (e.data.size > 0) chunks.push(e.data); };
    mr.start();
    var btn = audioBtn || videoBtn;
    btn.style.opacity = '1';
    btn.style.color = 'red';
    btn.onclick = function() {
      mr.onstop = function() {
        stream.getTracks().forEach(function(t) { t.stop(); });
        var blob = new Blob(chunks, { type: mode === 'video' ? 'video/webm' : 'audio/webm' });
        var file = new File([blob], mode + '_answer.webm', { type: blob.type });
        if (!answerPendingFiles[qid]) answerPendingFiles[qid] = [];
        answerPendingFiles[qid].push(file);
        var fl = form.querySelector('.answer-file-list');
        if (fl) fl.innerHTML += '<span>' + mode + ' rec</span> ';
        btn.style.opacity = '';
        btn.style.color = '';
        btn.onclick = null;
      };
      mr.stop();
    };
  }).catch(function() { alert('No access to ' + (mode === 'video' ? 'camera' : 'microphone')); });
});

// Admin: submit answer.
document.getElementById('questions').addEventListener('click', function(e) {
  var btn = e.target.closest('.answer-submit-btn');
  if (!btn || !isAdmin) return;
  var form = btn.closest('.q-answer-form');
  var qid = parseInt(form.dataset.qid);
  var textarea = form.querySelector('textarea');
  var body = textarea.value.trim();
  var files = answerPendingFiles[qid] || [];
  // also collect from file inputs
  var fileInputs = form.querySelectorAll('.answer-file');
  for (var fi = 0; fi < fileInputs.length; fi++) {
    if (fileInputs[fi].files) {
      for (var j = 0; j < fileInputs[fi].files.length; j++) {
        files.push(fileInputs[fi].files[j]);
      }
    }
  }
  if (!body && files.length === 0) return;

  var fd = new FormData();
  fd.append('body', body);
  for (var i = 0; i < files.length; i++) {
    fd.append('media', files[i]);
  }
  btn.disabled = true;
  fetch(basePath + '/questions/' + qid + '/answer', {
    method: 'POST',
    body: fd
  }).then(function(r) {
    if (!r.ok) throw new Error('status ' + r.status);
    return r.json();
  }).then(function(answer) {
    var q = questions.find(function(q) { return q.id === qid; });
    if (q) {
      if (!q.answers) q.answers = [];
      q.answers.push(answer);
      q.answered = true;
      answerVersionIndex[qid] = q.answers.length - 1;
    }
    answerPendingFiles[qid] = [];
    renderQuestions();
  }).catch(function(err) { console.error('answer submit:', err); }).finally(function() {
    btn.disabled = false;
  });
});

// Sort.
document.getElementById('sort').addEventListener('click', function(e) {
  var a = e.target.closest('a[data-sort]');
  if (!a) return;
  sortMode = a.dataset.sort;
  document.querySelectorAll('#sort a').forEach(function(x) {
    x.classList.toggle('active', x.dataset.sort === sortMode);
  });
  renderQuestions();
});

// Compose: textarea auto-grow and preview.
var input = document.getElementById('input');
var preview = document.getElementById('compose-preview');

function autoGrow() {
  input.style.height = 'auto';
  input.style.height = Math.max(80, input.scrollHeight) + 'px';
}
input.addEventListener('input', autoGrow);

var previewTimeout;
input.addEventListener('input', function() {
  clearTimeout(previewTimeout);
  previewTimeout = setTimeout(function() {
    var v = input.value.trim();
    if (!v) { preview.innerHTML = ''; return; }
    preview.innerHTML = renderMd(v);
    if (typeof renderMathInElement === 'function') {
      renderMathInElement(preview, {
        delimiters: [{ left: '$$', right: '$$', display: true }, { left: '$', right: '$', display: false }],
        throwOnError: false
      });
    }
  }, 300);
});

// Submit question.
var pendingFiles = [];

function submitQuestion() {
  var v = input.value.trim();
  if (!v && pendingFiles.length === 0) return;

  var fd = new FormData();
  fd.append('body', v);
  fd.append('voter_id', voterID);
  for (var i = 0; i < pendingFiles.length; i++) {
    fd.append('media', pendingFiles[i]);
  }

  var btn = document.getElementById('btn-submit');
  btn.disabled = true;
  fetch(basePath + '/questions', {
    method: 'POST',
    body: fd
  }).then(function(r) {
    if (!r.ok) throw new Error('status ' + r.status);
    return r.json();
  }).then(function(q) {
    questions.unshift(q);
    myQuestions.add(q.id);
    saveMyQuestions();
    input.value = '';
    preview.innerHTML = '';
    pendingFiles = [];
    document.getElementById('file-list').innerHTML = '';
    autoGrow();
    renderQuestions();
  }).catch(function(err) { console.error('submit:', err); }).finally(function() {
    btn.disabled = false;
  });
}

document.getElementById('btn-submit').addEventListener('click', submitQuestion);
input.addEventListener('keydown', function(e) {
  if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) { e.preventDefault(); submitQuestion(); }
});

// File attach.
document.getElementById('file-img').addEventListener('change', function() {
  if (this.files.length) {
    pendingFiles.push(this.files[0]);
    document.getElementById('file-list').innerHTML += '<span>' + esc(this.files[0].name) + '</span>';
    this.value = '';
  }
});
document.getElementById('file-any').addEventListener('change', function() {
  if (this.files.length) {
    pendingFiles.push(this.files[0]);
    document.getElementById('file-list').innerHTML += '<span>' + esc(this.files[0].name) + '</span>';
    this.value = '';
  }
});

// Recorder (audio OR video).
var recorder = document.getElementById('recorder');
var recCanvas = document.getElementById('rec-canvas');
var recCtx = recCanvas.getContext('2d');
var recTimerEl = document.getElementById('rec-timer');
var recPreview = document.getElementById('rec-preview');
var recStart = document.getElementById('rec-start');
var recPause = document.getElementById('rec-pause');
var recDone = document.getElementById('rec-done');
var recCancel = document.getElementById('rec-cancel');
var btnAudio = document.getElementById('btn-audio');
var btnVideo = document.getElementById('btn-video');

var recStream = null, recMode = null, recRecording = false, recPaused = false;
var recInterval = null, recSeconds = 0;
var audioCtx = null, analyser = null, animFrame = null;
var mediaRecorder = null, recordedChunks = [];

function openRecorder(mode) {
  if (recStream) closeRecorder();
  recMode = mode;
  var constraints = mode === 'video' ? { audio: true, video: true } : { audio: true };
  navigator.mediaDevices.getUserMedia(constraints).then(function(stream) {
    recStream = stream;
    recorder.classList.add('active');
    if (mode === 'video') {
      recorder.classList.add('video-mode');
      recPreview.srcObject = stream;
      recPreview.play();
    } else {
      recorder.classList.remove('video-mode');
    }
    btnAudio.classList.toggle('active', mode === 'audio');
    btnVideo.classList.toggle('active', mode === 'video');
    audioCtx = new (window.AudioContext || window.webkitAudioContext)();
    analyser = audioCtx.createAnalyser();
    analyser.fftSize = 256;
    audioCtx.createMediaStreamSource(stream).connect(analyser);
    drawScope();
  }).catch(function() { alert('No access to ' + (mode === 'video' ? 'camera' : 'microphone')); });
}

function drawScope() {
  var w = recCanvas.width = recCanvas.offsetWidth * (devicePixelRatio || 1);
  var h = recCanvas.height = recCanvas.offsetHeight * (devicePixelRatio || 1);
  recCtx.scale(devicePixelRatio || 1, devicePixelRatio || 1);
  var dw = recCanvas.offsetWidth, dh = recCanvas.offsetHeight;
  var buf = new Uint8Array(analyser.frequencyBinCount);
  (function draw() {
    animFrame = requestAnimationFrame(draw);
    analyser.getByteTimeDomainData(buf);
    recCtx.clearRect(0, 0, dw, dh);
    recCtx.beginPath();
    recCtx.strokeStyle = '#999';
    recCtx.lineWidth = 1;
    var step = dw / buf.length;
    for (var i = 0; i < buf.length; i++) {
      var y = (buf[i] / 255) * dh;
      i === 0 ? recCtx.moveTo(0, y) : recCtx.lineTo(i * step, y);
    }
    recCtx.stroke();
  })();
}

function closeRecorder() {
  if (mediaRecorder && mediaRecorder.state !== 'inactive') {
    mediaRecorder.stop();
  }
  mediaRecorder = null;
  recordedChunks = [];
  if (animFrame) { cancelAnimationFrame(animFrame); animFrame = null; }
  if (audioCtx) { audioCtx.close(); audioCtx = null; analyser = null; }
  if (recStream) { recStream.getTracks().forEach(function(t) { t.stop(); }); recStream = null; }
  recPreview.srcObject = null;
  clearInterval(recInterval);
  recorder.classList.remove('active', 'recording', 'video-mode');
  recRecording = false; recPaused = false; recSeconds = 0;
  recTimerEl.textContent = '0:00';
  recStart.disabled = false; recPause.disabled = true; recDone.disabled = true;
  btnAudio.classList.remove('active'); btnVideo.classList.remove('active');
  recMode = null;
}

btnAudio.addEventListener('click', function() { recMode === 'audio' ? closeRecorder() : openRecorder('audio'); });
btnVideo.addEventListener('click', function() { recMode === 'video' ? closeRecorder() : openRecorder('video'); });

recStart.addEventListener('click', function() {
  if (recRecording && !recPaused) return;
  if (!recRecording) {
    recordedChunks = [];
    var mimeType = recMode === 'video' ? 'video/webm' : 'audio/webm';
    mediaRecorder = new MediaRecorder(recStream, { mimeType: mimeType });
    mediaRecorder.ondataavailable = function(e) {
      if (e.data.size > 0) recordedChunks.push(e.data);
    };
    mediaRecorder.start(100);
  } else if (recPaused && mediaRecorder) {
    mediaRecorder.resume();
  }
  recRecording = true; recPaused = false;
  recorder.classList.add('recording');
  recStart.disabled = true; recPause.disabled = false; recDone.disabled = false;
  recInterval = setInterval(function() {
    recSeconds++;
    recTimerEl.textContent = Math.floor(recSeconds / 60) + ':' + String(recSeconds % 60).padStart(2, '0');
  }, 1000);
});

recPause.addEventListener('click', function() {
  if (!recRecording) return;
  recPaused = !recPaused;
  if (recPaused) {
    clearInterval(recInterval); recorder.classList.remove('recording'); recStart.disabled = false;
    if (mediaRecorder) mediaRecorder.pause();
  } else {
    recorder.classList.add('recording'); recStart.disabled = true;
    if (mediaRecorder) mediaRecorder.resume();
    recInterval = setInterval(function() {
      recSeconds++;
      recTimerEl.textContent = Math.floor(recSeconds / 60) + ':' + String(recSeconds % 60).padStart(2, '0');
    }, 1000);
  }
});

recDone.addEventListener('click', function() {
  if (!mediaRecorder) { closeRecorder(); return; }
  mediaRecorder.onstop = function() {
    var ext = recMode === 'video' ? '.webm' : '.webm';
    var mimeType = recMode === 'video' ? 'video/webm' : 'audio/webm';
    var blob = new Blob(recordedChunks, { type: mimeType });
    var file = new File([blob], recMode + '_recording' + ext, { type: mimeType });
    pendingFiles.push(file);
    document.getElementById('file-list').innerHTML += '<span>' + recMode + ' (' + recTimerEl.textContent + ')</span>';
    closeRecorder();
  };
  mediaRecorder.stop();
});
recCancel.addEventListener('click', closeRecorder);

// Impressum toggle.
var impressumLink = document.getElementById('link-impressum');
var backLink = document.getElementById('link-back');
if (impressumLink) {
  impressumLink.addEventListener('click', function(e) {
    e.preventDefault();
    ['questions', 'compose', 'sort'].forEach(function(id) {
      var el = document.getElementById(id);
      if (el) el.style.display = 'none';
    });
    var ft = document.querySelector('footer');
    if (ft) ft.style.display = 'none';
    document.getElementById('impressum-page').style.display = 'block';
  });
}
if (backLink) {
  backLink.addEventListener('click', function(e) {
    e.preventDefault();
    ['questions', 'compose', 'sort'].forEach(function(id) {
      var el = document.getElementById(id);
      if (el) el.style.display = '';
    });
    var ft = document.querySelector('footer');
    if (ft) ft.style.display = '';
    document.getElementById('impressum-page').style.display = 'none';
  });
}

// SSE: real-time updates.
function connectSSE() {
  var evtSource = new EventSource(basePath + '/events');
  evtSource.addEventListener('question_new', function(e) {
    var q = JSON.parse(e.data);
    if (!questions.find(function(x) { return x.id === q.id; })) {
      questions.push(q);
      renderQuestions();
    }
  });
  evtSource.addEventListener('question_vote', function(e) {
    var data = JSON.parse(e.data);
    var q = questions.find(function(x) { return x.id === data.question_id; });
    if (q) { q.vote_count = data.vote_count; renderQuestions(); }
  });
  evtSource.addEventListener('question_delete', function(e) {
    var data = JSON.parse(e.data);
    questions = questions.filter(function(q) { return q.id !== data.question_id; });
    renderQuestions();
  });
  evtSource.addEventListener('question_update', function(e) {
    var data = JSON.parse(e.data);
    var q = questions.find(function(x) { return x.id === data.id; });
    if (q) { q.body = data.body; q.original_body = data.original_body; renderQuestions(); }
  });
  evtSource.addEventListener('answer_new', function(e) {
    var data = JSON.parse(e.data);
    var q = questions.find(function(x) { return x.id === data.question_id; });
    if (q && data.answers) {
      q.answers = data.answers;
      q.answered = true;
      answerVersionIndex[q.id] = q.answers.length - 1;
      renderQuestions();
    }
  });
  evtSource.addEventListener('answer_vote', function(e) {
    var data = JSON.parse(e.data);
    for (var i = 0; i < questions.length; i++) {
      var answers = questions[i].answers;
      if (!answers) continue;
      for (var j = 0; j < answers.length; j++) {
        if (answers[j].id === data.answer_id) {
          answers[j].thumbs_up = data.thumbs_up;
          answers[j].thumbs_down = data.thumbs_down;
          renderQuestions();
          return;
        }
      }
    }
  });
  evtSource.onerror = function() {
    evtSource.close();
    setTimeout(connectSSE, 3000);
  };
}

// Initialize.
fetchQuestions();
connectSSE();

})();
