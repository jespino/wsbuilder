/* wsbuilder default theme. Everything here is an enhancement: the content,
 * links and downloads all work without JavaScript. */
(function () {
  var root = document.documentElement;
  var prefix = root.dataset.storage + ":";

  function read(name) {
    try { return localStorage.getItem(prefix + name); } catch (e) { return null; }
  }
  function write(name, value) {
    try { localStorage.setItem(prefix + name, value); } catch (e) {}
  }
  function all(selector, scope) {
    return Array.prototype.slice.call((scope || document).querySelectorAll(selector));
  }

  // Theme ---------------------------------------------------------------------
  function setupTheme() {
    var button = document.querySelector("[data-theme-toggle]");
    if (!button) return;
    button.hidden = false;
    button.addEventListener("click", function () {
      var dark = root.dataset.theme
        ? root.dataset.theme === "dark"
        : matchMedia("(prefers-color-scheme: dark)").matches;
      root.dataset.theme = dark ? "light" : "dark";
      write("theme", root.dataset.theme);
    });
  }

  // Tracks --------------------------------------------------------------------
  function setupTracks() {
    var select = document.querySelector("[data-track-select]");
    if (!select) return;
    select.value = root.dataset.track;
    document.querySelector("[data-track-picker]").hidden = false;
    select.addEventListener("change", function () {
      root.dataset.track = select.value;
      write("track", select.value);
    });
  }

  // Progress ------------------------------------------------------------------
  function progress() {
    try { return JSON.parse(read("progress")) || {}; } catch (e) { return {}; }
  }

  function renderProgress() {
    var done = progress();
    all("[data-module-link]").forEach(function (link) {
      link.classList.toggle("is-done", !!done[link.dataset.moduleLink]);
    });
    all("[data-progress]").forEach(function (pill) {
      var slugs = pill.dataset.slugs.split(",");
      var count = slugs.filter(function (slug) { return done[slug]; }).length;
      pill.textContent = count + "/" + slugs.length + " " + pill.dataset.label;
      pill.hidden = false;
    });
  }

  function setupComplete() {
    var slug = document.body.dataset.module;
    var button = document.querySelector("[data-complete]");
    if (!slug || !button) return;
    function refresh() {
      var done = !!progress()[slug];
      button.textContent = done ? button.dataset.doneLabel : button.dataset.label;
      button.classList.toggle("is-done", done);
    }
    button.hidden = false;
    button.addEventListener("click", function () {
      var state = progress();
      if (state[slug]) delete state[slug]; else state[slug] = Date.now();
      write("progress", JSON.stringify(state));
      refresh();
      renderProgress();
    });
    refresh();
  }

  // Task lists in a module stay checked between visits ------------------------
  function setupTasks() {
    var slug = document.body.dataset.module;
    if (!slug) return;
    var key = "tasks:" + slug;
    var state = {};
    try { state = JSON.parse(read(key)) || {}; } catch (e) {}
    all(".prose input[type=checkbox]").forEach(function (box, index) {
      box.disabled = false;
      if (index in state) box.checked = state[index];
      box.addEventListener("change", function () {
        state[index] = box.checked;
        write(key, JSON.stringify(state));
      });
    });
  }

  // Code ----------------------------------------------------------------------
  function setupCopy() {
    if (!navigator.clipboard) return;
    var body = document.body.dataset;
    all(".code-block").forEach(function (block) {
      var code = block.querySelector("code");
      var button = document.createElement("button");
      button.type = "button";
      button.className = "copy-button";
      button.textContent = body.copy;
      button.addEventListener("click", function () {
        navigator.clipboard.writeText(code.textContent).then(function () {
          button.textContent = body.copied;
          setTimeout(function () { button.textContent = body.copy; }, 1500);
        });
      });
      block.appendChild(button);
    });
  }

  // Headings get a link to themselves -----------------------------------------
  function setupAnchors() {
    all(".prose h2[id], .prose h3[id]").forEach(function (heading) {
      var link = document.createElement("a");
      link.className = "heading-anchor";
      link.href = "#" + heading.id;
      link.setAttribute("aria-hidden", "true");
      link.tabIndex = -1;
      link.textContent = "#";
      heading.appendChild(link);
    });
  }

  // The table of contents follows the reader ----------------------------------
  function setupScrollSpy() {
    var links = all(".toc a");
    if (!links.length || !window.IntersectionObserver) return;
    var byId = {};
    links.forEach(function (link) { byId[link.hash.slice(1)] = link; });
    var observer = new IntersectionObserver(function (entries) {
      entries.forEach(function (entry) {
        if (!entry.isIntersecting) return;
        links.forEach(function (link) { link.classList.remove("is-active"); });
        var active = byId[entry.target.id];
        if (active) active.classList.add("is-active");
      });
    }, { rootMargin: "-10% 0px -80% 0px" });
    Object.keys(byId).forEach(function (id) {
      var target = document.getElementById(id);
      if (target) observer.observe(target);
    });
  }

  // Menus close when the reader clicks elsewhere ------------------------------
  function setupMenus() {
    document.addEventListener("click", function (event) {
      all("details.menu[open]").forEach(function (menu) {
        if (!menu.contains(event.target)) menu.open = false;
      });
    });
  }

  setupTheme();
  setupTracks();
  setupComplete();
  setupTasks();
  setupCopy();
  setupAnchors();
  setupScrollSpy();
  setupMenus();
  renderProgress();
})();
