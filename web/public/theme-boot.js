(function () {
  var t = localStorage.getItem('lk-theme')
  var dark = t === 'dark'
  document.documentElement.classList.add('page-aliyun')
  document.documentElement.classList.toggle('dark', dark)
  var m = document.querySelector('meta[name="theme-color"]')
  if (m) m.setAttribute('content', dark ? '#141414' : '#F7F8FA')
})()
