$(document).ready(function() {
  CheckStatus();
});

var CheckStatus = function() {
  const data = {action: 'status'};
  request(data, (res)=>{
    UpdateApplicationList(res);
  }, (e)=>{
    console.log(e.responseJSON.message);
  });
};

var CreateStack = function(elm, name) {
  $(elm).addClass("disabled").addClass("loading");
  const data = {action: 'create', name: name};
  request(data, (res)=>{
    console.log(res);
    window.setTimeout(() => location.reload(true), 1000);
  }, (e)=>{
    console.log(e.responseJSON.message);
  });
};

var DeleteStack = function(elm, name) {
  $(elm).addClass("disabled").addClass("loading");
  const data = {action: 'delete', name: name};
  request(data, (res)=>{
    console.log(res);
    window.setTimeout(() => location.reload(true), 1000);
  }, (e)=>{
    console.log(e.responseJSON.message);
  });
};

var request = function(data, callback, onerror) {
  $.ajax({
    type:          'POST',
    dataType:      'json',
    contentType:   'application/json',
    scriptCharset: 'utf-8',
    data:          JSON.stringify(data),
    url:           App.url
  })
  .done(function(res) {
    callback(res);
  })
  .fail(function(e) {
    onerror(e);
  });
};

var UpdateApplicationList = function(obj) {
  loading = false;
  $("#item_container div").remove()
  obj.applicationList.forEach(application => {
    var nameTag
    if (application.stack.url === "") {
      nameTag = $("<div></div>", {
        "class": "header"
      }).text(application.name);
    } else {
      nameTag = $("<a></a>", {
        "class": "header",
        "href": application.stack.url
      }).text(application.name);
    }
    var descriptionTag = $("<div></div>", {
      "class": "description"
    }).text(application.description);
    var contentTag = $("<div></div>", {
      "class": "content"
    }).append(nameTag).append(descriptionTag);
    var buttonTag
    if (application.stack.status === "") {
      buttonTag = $("<div></div>", {
        "class": "ui green button",
        "onclick": "CreateStack(this, '" + application.name + "');"
      }).text("Create");
    } else if (application.stack.status === "CREATE_COMPLETE") {
      buttonTag = $("<div></div>", {
        "class": "ui red button",
        "onclick": "DeleteStack(this, '" + application.stack.name + "');"
      }).text("Delete");
    } else {
      loading = true;
      buttonTag = $("<div></div>", {
        "class": "ui teal disabled loading button"
      });
    }
    var floatedTag = $("<div></div>", {
      "class": "right floated content"
    }).append(buttonTag);
    var iconTag = $("<i></i>", {
      "class": "large hdd outline middle aligned icon"
    });
    var itemTag = $("<div></div>", {
      "class": "item"
    }).append(floatedTag).append(iconTag).append(contentTag);
    $("#item_container").append(itemTag);
  });
  if (loading) {
    window.setTimeout(() => location.reload(true), 60000);
  }
};
var App = { url: location.origin + {{ .ApiPath }} };
