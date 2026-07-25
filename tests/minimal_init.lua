-- Minimal Neovim init for the Lua suite.
--
-- Tier 1, per doc/adr/0001-testing-strategy.md: plenary's busted runner, one
-- `git clone --depth 1` and no LuaRocks. Run it with:
--
--     make test-lua
--
-- plenary is resolved in this order:
--   1. $CLEW_PLENARY_DIR, for a checkout you already have
--   2. .tests/site/pack/deps/start/plenary.nvim, cloned on first run
--
-- Everything it writes lives under .tests/, which is gitignored.

local this = debug.getinfo(1, "S").source:sub(2)
local root = vim.fn.fnamemodify(this, ":p:h:h")
local site = root .. "/.tests/site"
local plenary = vim.env.CLEW_PLENARY_DIR or (site .. "/pack/deps/start/plenary.nvim")

if vim.fn.isdirectory(plenary) == 0 then
  vim.fn.mkdir(vim.fn.fnamemodify(plenary, ":h"), "p")
  print("cloning plenary.nvim into " .. plenary)
  local out = vim.fn.system({
    "git", "clone", "--depth", "1",
    "https://github.com/nvim-lua/plenary.nvim", plenary,
  })
  if vim.v.shell_error ~= 0 then
    error("could not clone plenary.nvim:\n" .. out)
  end
end

-- Keep the suite isolated from the developer's own configuration: a plugin
-- manager on the runtimepath would load half their editor into these tests.
vim.env.XDG_CONFIG_HOME = root .. "/.tests/config"
vim.env.XDG_DATA_HOME = root .. "/.tests/data"
vim.env.XDG_STATE_HOME = root .. "/.tests/state"

vim.opt.runtimepath:append(root)
vim.opt.runtimepath:append(plenary)
vim.opt.packpath = { site }
vim.opt.swapfile = false

-- `require("tests.helpers")` from a spec. The runtimepath only reaches lua/,
-- and tests/ deliberately does not ship inside it.
package.path = table.concat({
  root .. "/?.lua",
  root .. "/?/init.lua",
  package.path,
}, ";")

vim.cmd("runtime plugin/plenary.vim")
