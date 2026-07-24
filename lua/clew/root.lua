--- Project root detection.
---
--- This is the most important module in the plugin, and the one place where the
--- obvious implementation is wrong.
---
--- Neovim's built-in `root_markers` resolves to the NEAREST ancestor containing a
--- marker. In a git-submodule umbrella that is catastrophic: opening
--- `java/svc-a/src/main/java/Foo.java` matches `java/svc-a/pom.xml` first, so you
--- get one language server per submodule -- precisely the resource explosion clew
--- exists to avoid, and it also breaks navigation *between* submodules because
--- each server only knows its own subtree.
---
--- clew therefore walks all the way to the filesystem root and keeps the
--- OUTERMOST directory containing a marker, with one exception: an explicit
--- `.clew/config.toml` always wins, so users can pin a root when the heuristic
--- guesses wrong (nested umbrellas, monorepo-inside-a-monorepo, ...).

local M = {}

local uv = vim.uv or vim.loop

---@param path string
---@return boolean
local function exists(path)
  return uv.fs_stat(path) ~= nil
end

--- Ancestors of `start`, innermost first, including `start` itself.
---@param start string
---@return string[]
local function ancestors(start)
  local out = {}
  local dir = vim.fs.normalize(start)
  while true do
    table.insert(out, dir)
    local parent = vim.fs.dirname(dir)
    if not parent or parent == dir then break end
    dir = parent
  end
  return out
end

--- Resolve the project root for a given path.
---
--- Precedence:
---   1. the outermost directory containing `.clew/config.toml` (explicit pin)
---   2. the outermost directory containing any configured marker
---   3. nil
---
---@param start string       File or directory to resolve from.
---@param markers string[]   Marker paths, relative to a candidate directory.
---@return string|nil root
---@return string|nil matched_marker
function M.find(start, markers)
  local stat = uv.fs_stat(start)
  local from = (stat and stat.type == "directory") and start or vim.fs.dirname(start)
  if not from then return nil, nil end

  local chain = ancestors(from)

  -- Pass 1: an explicit pin always wins, outermost first.
  for i = #chain, 1, -1 do
    if exists(vim.fs.joinpath(chain[i], ".clew", "config.toml")) then
      return chain[i], ".clew/config.toml"
    end
  end

  -- Pass 2: outermost directory holding any marker.
  for i = #chain, 1, -1 do
    for _, marker in ipairs(markers) do
      if exists(vim.fs.joinpath(chain[i], marker)) then
        return chain[i], marker
      end
    end
  end

  return nil, nil
end

--- Build a `root_dir` function suitable for `vim.lsp.config`.
---@param markers string[]
---@return fun(bufnr: integer, on_dir: fun(dir: string|nil))
function M.root_dir_fn(markers)
  return function(bufnr, on_dir)
    local name = vim.api.nvim_buf_get_name(bufnr)
    if name == "" then
      on_dir(nil)
      return
    end
    on_dir((M.find(name, markers)))
  end
end

return M
