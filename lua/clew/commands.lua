local config = require("clew.config")
local root = require("clew.root")
local binary = require("clew.binary")
local lsp = require("clew.lsp")

local M = {}

local uv = vim.uv or vim.loop

---@param msg string
---@param level integer|nil
local function notify(msg, level)
  vim.notify(msg, level or vim.log.levels.INFO, { title = "clew" })
end

--- Resolve the project root for the current buffer.
---@return string|nil root
---@return string|nil marker
local function current_root()
  local name = vim.api.nvim_buf_get_name(0)
  if name == "" then name = uv.cwd() end
  return root.find(name, config.options.root.markers)
end

--- `:ClewIndex [unit]`
---@param unit string|nil
function M.index(unit)
  local cfg = config.options
  local project = current_root()
  if not project then
    notify("no clew project root found for this buffer", vim.log.levels.ERROR)
    return
  end

  local cmd = binary.index_cmd(cfg, project, unit)
  if not cmd then
    notify("clew binary not found", vim.log.levels.ERROR)
    return
  end

  notify(("indexing %s%s ..."):format(vim.fs.basename(project), unit and (" [" .. unit .. "]") or ""))

  local started = os.time()
  vim.system(cmd, { text = true }, function(res)
    vim.schedule(function()
      local secs = os.time() - started
      if res.code ~= 0 then
        notify(("indexing failed (exit %d)\n%s"):format(res.code, res.stderr or ""), vim.log.levels.ERROR)
        return
      end
      notify(("index rebuilt in %ds"):format(secs))
      -- Reload so the server picks up the new index immediately.
      for _, client in ipairs(lsp.clients()) do
        client:notify("workspace/didChangeWatchedFiles", {
          changes = { { uri = vim.uri_from_fname(vim.fs.joinpath(project, cfg.index_path)), type = 2 } },
        })
      end
    end)
  end)
end

--- `:ClewStatus`
function M.status()
  local cfg = config.options
  local project, marker = current_root()
  local bin, source = binary.find()

  local lines = { "# clew" }
  table.insert(lines, ("root      : %s"):format(project or "<not found>"))
  if marker then table.insert(lines, ("matched   : %s"):format(marker)) end
  table.insert(lines, ("binary    : %s%s"):format(bin or "<not found>", source and (" (" .. source .. ")") or ""))

  if project then
    local index = vim.fs.joinpath(project, cfg.index_path)
    local stat = uv.fs_stat(index)
    if stat then
      local age = os.time() - stat.mtime.sec
      table.insert(lines, ("index     : %s"):format(index))
      table.insert(lines, ("size      : %.1f MB"):format(stat.size / 1024 / 1024))
      table.insert(lines, ("age       : %s"):format(M._humanise(age)))
    else
      table.insert(lines, ("index     : <missing> (run :ClewIndex)"))
    end
  end

  local clients = lsp.clients()
  table.insert(lines, ("clients   : %d"):format(#clients))

  notify(table.concat(lines, "\n"))
end

---@param secs integer
---@return string
function M._humanise(secs)
  if secs < 60 then return secs .. "s ago" end
  if secs < 3600 then return math.floor(secs / 60) .. "m ago" end
  if secs < 86400 then return math.floor(secs / 3600) .. "h ago" end
  return math.floor(secs / 86400) .. "d ago"
end

--- `:ClewRestart`
function M.restart()
  local n = lsp.stop()
  notify(("stopped %d client(s); reattaching on next buffer"):format(n))
  vim.cmd("edit")
end

function M.setup()
  vim.api.nvim_create_user_command("ClewIndex", function(o)
    M.index(o.args ~= "" and o.args or nil)
  end, { nargs = "?", desc = "clew: build or rebuild the project index" })

  vim.api.nvim_create_user_command("ClewStatus", function()
    M.status()
  end, { desc = "clew: show root, index age and client status" })

  vim.api.nvim_create_user_command("ClewRestart", function()
    M.restart()
  end, { desc = "clew: restart the language server" })
end

return M
